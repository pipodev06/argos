//go:build windows

package checks

import (
	"context"
	"encoding/binary"
	"net/netip"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// icmpSendEcho usa IcmpSendEcho de iphlpapi.dll: ICMP sin privilegios. Solo IPv4
// (IPv6 pasa al ping del SO).
type icmpSendEcho struct {
	create, send, closeHandle *windows.LazyProc
}

func newNativePinger() Pinger {
	dll := windows.NewLazySystemDLL("iphlpapi.dll")
	return &icmpSendEcho{
		create:      dll.NewProc("IcmpCreateFile"),
		send:        dll.NewProc("IcmpSendEcho"),
		closeHandle: dll.NewProc("IcmpCloseHandle"),
	}
}

const ipSuccess = 0 // IP_SUCCESS en ICMP_ECHO_REPLY.Status

func (p *icmpSendEcho) Ping(ctx context.Context, ip netip.Addr) (time.Duration, error) {
	ip = ip.Unmap()
	if !ip.Is4() || p.send.Find() != nil {
		return 0, ErrPingUnavailable
	}
	timeout := 2 * time.Second
	if dl, ok := ctx.Deadline(); ok {
		timeout = time.Until(dl)
	}
	if timeout <= 0 || ctx.Err() != nil {
		return 0, ErrPingNoReply
	}
	h, _, _ := p.create.Call()
	if h == 0 || h == ^uintptr(0) {
		return 0, ErrPingUnavailable
	}
	defer func() { _, _, _ = p.closeHandle.Call(h) }() // cierre best-effort del handle propio

	a := ip.As4()
	dest := binary.LittleEndian.Uint32(a[:]) // in_addr en orden de red
	payload := []byte("argos-ping")
	reply := make([]byte, 256)
	n, _, _ := p.send.Call(h, uintptr(dest),
		uintptr(unsafe.Pointer(&payload[0])), uintptr(len(payload)), 0, //nolint:gosec // G103: syscall de Windows; los buffers viven hasta el retorno
		uintptr(unsafe.Pointer(&reply[0])), uintptr(len(reply)), uintptr(timeout.Milliseconds())) //nolint:gosec // G103: idem
	// ICMP_ECHO_REPLY: Address u32 @0, Status u32 @4, RoundTripTime u32 @8.
	if n == 0 || binary.LittleEndian.Uint32(reply[4:8]) != ipSuccess {
		return 0, ErrPingNoReply
	}
	return time.Duration(binary.LittleEndian.Uint32(reply[8:12])) * time.Millisecond, nil
}
