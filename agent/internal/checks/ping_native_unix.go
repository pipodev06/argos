//go:build linux || darwin

package checks

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

// icmpPinger usa un socket ICMP de datagrama (sin root): en Linux exige que
// net.ipv4.ping_group_range incluya el GID del servicio; macOS lo permite. Solo IPv4
// (IPv6 pasa al ping del SO). Si el socket no se puede abrir, no esta disponible.
type icmpPinger struct{ seq atomic.Uint32 }

func newNativePinger() Pinger { return &icmpPinger{} }

const recvSlice = 200 * time.Millisecond // para revisar ctx entre lecturas

func (p *icmpPinger) Ping(ctx context.Context, ip netip.Addr) (time.Duration, error) {
	ip = ip.Unmap()
	if !ip.Is4() {
		return 0, ErrPingUnavailable
	}
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, unix.IPPROTO_ICMP)
	if err != nil {
		return 0, fmt.Errorf("abriendo socket icmp: %w", ErrPingUnavailable)
	}
	defer func() { _ = unix.Close(fd) }() // cierre best-effort del socket propio
	unix.CloseOnExec(fd)

	seq := uint16(p.seq.Add(1) & 0xffff) // contador que envuelve a 16 bits a proposito
	start := time.Now()
	if err := unix.Sendto(fd, echoRequest(seq), 0, &unix.SockaddrInet4{Addr: ip.As4()}); err != nil {
		if errors.Is(err, unix.EPERM) || errors.Is(err, unix.EACCES) {
			return 0, fmt.Errorf("enviando echo: %w", ErrPingUnavailable)
		}
		return 0, ErrPingNoReply
	}
	buf := make([]byte, 1500)
	for {
		if ctx.Err() != nil {
			return 0, ErrPingNoReply
		}
		wait := recvSlice
		if dl, ok := ctx.Deadline(); ok {
			wait = min(wait, time.Until(dl))
		}
		if wait <= 0 {
			return 0, ErrPingNoReply
		}
		tv := unix.NsecToTimeval(wait.Nanoseconds())
		if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv); err != nil {
			return 0, ErrPingNoReply
		}
		n, _, err := unix.Recvfrom(fd, buf, 0)
		switch {
		case err == nil:
			if isEchoReply(buf[:n], seq) {
				return time.Since(start), nil
			}
		case errors.Is(err, unix.EAGAIN), errors.Is(err, unix.EINTR):
			// plazo de la rebanada: se revisa ctx y se sigue esperando
		default:
			return 0, ErrPingNoReply
		}
	}
}

// echoRequest arma un echo ICMPv4 (tipo 8) con checksum; el kernel pone el id.
func echoRequest(seq uint16) []byte {
	b := []byte{8, 0, 0, 0, 0, 0, 0, 0, 'a', 'r', 'g', 'o', 's', 0, 0, 0}
	binary.BigEndian.PutUint16(b[6:8], seq)
	binary.BigEndian.PutUint16(b[2:4], checksum(b))
	return b
}

func checksum(b []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(b); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(b[i:]))
	}
	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = sum&0xffff + sum>>16
	}
	return ^uint16(sum)
}

// isEchoReply acepta un echo reply (tipo 0) con nuestro seq; en macOS el datagrama
// trae la cabecera IP delante.
func isEchoReply(b []byte, seq uint16) bool {
	if len(b) > 0 && b[0]>>4 == 4 {
		ihl := int(b[0]&0x0f) * 4
		if ihl < 20 || len(b) < ihl {
			return false
		}
		b = b[ihl:]
	}
	return len(b) >= 8 && b[0] == 0 && binary.BigEndian.Uint16(b[6:8]) == seq
}
