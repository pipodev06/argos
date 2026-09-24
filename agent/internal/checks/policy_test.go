package checks

import (
	"net/netip"
	"testing"
)

func TestParsePolicy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		raw     string
		entries int
		wantErr bool
	}{
		{"", 0, false},
		{" , ", 0, false},
		{"10.0.0.5", 1, false},
		{"10.0.0.5:22", 1, false},
		{"10.0.0.0/24", 1, false},
		{"10.0.0.0/24:443", 1, false},
		{"db.internal, cache.internal:6379", 2, false},
		{"::1, [fd00::5]:22", 2, false},
		{"bad host", 0, true},
		{"host:0", 0, true},
		{"host:99999", 0, true},
		{"10.0.0.5:abc", 0, true},
		{"http://x", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			t.Parallel()
			p, err := ParsePolicy(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && len(p.entries) != tt.entries {
				t.Errorf("entradas = %d, want %d", len(p.entries), tt.entries)
			}
		})
	}
}

func TestPolicyAllows(t *testing.T) {
	t.Parallel()
	own := []netip.Addr{netip.MustParseAddr("192.168.1.10")}
	mustPolicy := func(raw string) *Policy {
		p, err := ParsePolicy(raw)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	tests := []struct {
		name string
		p    *Policy
		host string
		ip   string
		port uint16
		want bool
	}{
		{"defecto: loopback", nil, "localhost", "127.0.0.1", 80, true},
		{"defecto: loopback v6", nil, "localhost", "::1", 80, true},
		{"defecto: loopback mapeado", nil, "x", "::ffff:127.0.0.1", 80, true},
		{"defecto: IP propia", nil, "mi-equipo", "192.168.1.10", 22, true},
		{"defecto: privada ajena", nil, "10.0.0.5", "10.0.0.5", 22, false},
		{"defecto: metadata de nube", nil, "x", "169.254.169.254", 80, false},
		{"defecto: publica", nil, "example.com", "93.184.216.34", 443, false},
		{"vacia = defecto", mustPolicy(""), "10.0.0.5", "10.0.0.5", 22, false},
		{"IP en allowlist", mustPolicy("10.0.0.5"), "10.0.0.5", "10.0.0.5", 22, true},
		{"IP con puerto: puerto ok", mustPolicy("10.0.0.5:22"), "10.0.0.5", "10.0.0.5", 22, true},
		{"IP con puerto: otro puerto", mustPolicy("10.0.0.5:22"), "10.0.0.5", "10.0.0.5", 3306, false},
		{"CIDR", mustPolicy("10.0.0.0/24"), "x", "10.0.0.77", 80, true},
		{"fuera del CIDR", mustPolicy("10.0.0.0/24"), "x", "10.0.1.1", 80, false},
		{"hostname exacto", mustPolicy("db.internal"), "DB.internal.", "10.9.9.9", 5432, true},
		{"hostname distinto", mustPolicy("db.internal"), "otro.internal", "10.9.9.9", 5432, false},
		{"hostname con puerto ajeno", mustPolicy("db.internal:5432"), "db.internal", "10.9.9.9", 22, false},
		{"IP invalida", nil, "x", "invalid IP", 80, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ip, _ := netip.ParseAddr(tt.ip) // "invalid IP" da el cero, que se rechaza
			if got := tt.p.allows(tt.host, ip, tt.port, own); got != tt.want {
				t.Errorf("allows = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestControlBlocksResolvedIP(t *testing.T) {
	t.Parallel()
	p, err := ParsePolicy("")
	if err != nil {
		t.Fatal(err)
	}
	ctl := p.control("svc.test", nil)
	tests := []struct {
		addr    string
		wantErr bool
	}{
		{"127.0.0.1:80", false},
		{"10.0.0.5:22", true},
		{"[::ffff:10.0.0.5]:22", true},
		{"no-es-ip:80", true},
		{"sin-puerto", true},
	}
	for _, tt := range tests {
		if err := ctl("tcp", tt.addr, nil); (err != nil) != tt.wantErr {
			t.Errorf("control(%q) err = %v, wantErr %v", tt.addr, err, tt.wantErr)
		}
	}
}
