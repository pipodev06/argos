//go:build linux || darwin

package checks

import (
	"encoding/binary"
	"testing"
)

func TestEchoRequestChecksumIsValid(t *testing.T) {
	t.Parallel()
	b := echoRequest(7)
	if b[0] != 8 || b[1] != 0 || binary.BigEndian.Uint16(b[6:8]) != 7 {
		t.Errorf("cabecera = %v, want echo request seq 7", b[:8])
	}
	if got := checksum(b); got != 0 { // con el checksum puesto, el total complementado es 0
		t.Errorf("checksum de verificacion = %#x, want 0", got)
	}
}

func TestIsEchoReply(t *testing.T) {
	t.Parallel()
	reply := []byte{0, 0, 0, 0, 0, 1, 0, 9} // tipo 0, seq 9
	withIP := append([]byte{0x45, 0, 0, 28, 0, 0, 0, 0, 64, 1, 0, 0, 10, 0, 0, 1, 10, 0, 0, 2}, reply...)
	tests := []struct {
		name string
		b    []byte
		seq  uint16
		want bool
	}{
		{"linux sin cabecera IP", reply, 9, true},
		{"macOS con cabecera IP", withIP, 9, true},
		{"otro seq", reply, 10, false},
		{"no es reply", []byte{3, 1, 0, 0, 0, 0, 0, 9}, 9, false},
		{"corto", []byte{0, 0}, 9, false},
		{"IHL absurdo", []byte{0x41, 0, 0, 0}, 9, false},
	}
	for _, tt := range tests {
		if got := isEchoReply(tt.b, tt.seq); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}
