package checks

import (
	"net"
	"sync"
	"testing"
)

// tlv codifica un elemento BER (longitud corta o larga).
func tlv(tag byte, val []byte) []byte {
	n := len(val)
	var hdr []byte
	switch {
	case n < 128:
		hdr = []byte{tag, byte(n)}
	case n < 256:
		hdr = []byte{tag, 0x81, byte(n)}
	default:
		hdr = []byte{tag, 0x82, byte(n >> 8), byte(n)}
	}
	return append(hdr, val...)
}

// readTLV lee un elemento BER (solo longitudes de hasta 2 bytes).
func readTLV(b []byte) (tag byte, val, rest []byte, ok bool) {
	if len(b) < 2 {
		return 0, nil, nil, false
	}
	tag, n, off := b[0], int(b[1]), 2
	if n&0x80 != 0 {
		k := n & 0x7f
		if k < 1 || k > 2 || len(b) < 2+k {
			return 0, nil, nil, false
		}
		n = 0
		for _, c := range b[2 : 2+k] {
			n = n<<8 | int(c)
		}
		off = 2 + k
	}
	if len(b) < off+n {
		return 0, nil, nil, false
	}
	return tag, b[off : off+n], b[off+n:], true
}

// v2cRequest extrae comunidad, request-id y OID (bytes BER) de un GET v2c; ok=false
// si el datagrama no es un GET v2c.
func v2cRequest(pkt []byte) (community, reqID, oid []byte, ok bool) {
	_, body, _, ok := readTLV(pkt)
	if !ok {
		return nil, nil, nil, false
	}
	_, ver, body, ok := readTLV(body)
	if !ok || len(ver) != 1 || ver[0] != 1 {
		return nil, nil, nil, false
	}
	_, community, body, ok = readTLV(body)
	if !ok {
		return nil, nil, nil, false
	}
	_, pdu, _, ok := readTLV(body)
	if !ok {
		return nil, nil, nil, false
	}
	_, reqID, pdu, ok = readTLV(pdu)
	if !ok {
		return nil, nil, nil, false
	}
	for range 2 { // error-status y error-index
		if _, _, pdu, ok = readTLV(pdu); !ok {
			return nil, nil, nil, false
		}
	}
	_, vbl, _, ok := readTLV(pdu)
	if !ok {
		return nil, nil, nil, false
	}
	_, vb, _, ok := readTLV(vbl)
	if !ok {
		return nil, nil, nil, false
	}
	_, oid, _, ok = readTLV(vb)
	return community, reqID, oid, ok
}

// v2cResponse arma la respuesta v2c con un unico varbind (valor ya codificado).
func v2cResponse(community, reqID, oid, value []byte) []byte {
	vb := tlv(0x30, concat(tlv(0x06, oid), value))
	pdu := tlv(0xA2, concat(tlv(0x02, reqID), tlv(0x02, []byte{0}), tlv(0x02, []byte{0}), tlv(0x30, vb)))
	return tlv(0x30, concat(tlv(0x02, []byte{1}), tlv(0x04, community), pdu))
}

func concat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// snmpSim es un agente UDP minimo en loopback. handler recibe cada datagrama y devuelve
// la respuesta (nil = silencio). Guarda todo lo recibido.
type snmpSim struct {
	port uint16
	mu   sync.Mutex
	got  [][]byte
}

func startSNMPSim(t *testing.T, handler func(pkt []byte) []byte) *snmpSim {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &snmpSim{port: uint16(conn.LocalAddr().(*net.UDPAddr).Port)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 65535)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			pkt := append([]byte(nil), buf[:n]...)
			s.mu.Lock()
			s.got = append(s.got, pkt)
			s.mu.Unlock()
			if resp := handler(pkt); resp != nil {
				_, _ = conn.WriteToUDP(resp, from) // el simulador no reintenta: el cliente decide
			}
		}
	}()
	t.Cleanup(func() { _ = conn.Close(); <-done })
	return s
}

func (s *snmpSim) packets() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]byte(nil), s.got...)
}
