package checks

import (
	"net/netip"
	"strings"
	"testing"
)

func TestProbePolicyAllows(t *testing.T) {
	t.Parallel()
	pol, err := ParseProbePolicy("192.168.1.0/24, 10.0.5.7, 2001:db8::/120", 1024)
	if err != nil {
		t.Fatalf("ParseProbePolicy: %v", err)
	}
	tests := []struct {
		ip   string
		want bool
	}{
		{"192.168.1.10", true},
		{"192.168.1.0", true},
		{"192.168.1.255", false}, // broadcast dirigido del /24
		{"192.168.2.10", false},  // CIDR fuera de la allowlist
		{"10.0.5.7", true},
		{"10.0.5.8", false},
		{"::ffff:192.168.1.10", true},  // IPv4-mapped dentro del rango
		{"::ffff:192.168.2.10", false}, // IPv4-mapped fuera del rango
		{"::ffff:127.0.0.1", false},    // IPv4-mapped en rango prohibido
		{"127.0.0.1", false},
		{"::1", false},
		{"169.254.169.254", false}, // metadata cloud
		{"fe80::1", false},
		{"224.0.0.1", false},
		{"255.255.255.255", false},
		{"0.0.0.0", false},
		{"2001:db8::5", true},
		{"2001:db8::1:5", false},
	}
	for _, tt := range tests {
		if got := pol.Allows(netip.MustParseAddr(tt.ip)); got != tt.want {
			t.Errorf("Allows(%s) = %v, want %v", tt.ip, got, tt.want)
		}
	}
	if pol.Allows(netip.Addr{}) {
		t.Error("una IP invalida no debe permitirse")
	}
	if pol.Allows(netip.MustParseAddr("fe80::1%eth0")) {
		t.Error("una IP con zona no debe permitirse")
	}
}

func TestForbiddenRangesWinOverAllowlist(t *testing.T) {
	t.Parallel()
	// Aun con una entrada que llegue a esas IPs (entrada de una sola IP), Allows las rechaza.
	for _, raw := range []string{"127.0.0.1", "169.254.169.254", "169.254.1.0/24", "127.0.0.0/24", "224.0.0.0/24", "255.255.255.255", "0.0.0.0", "fe80::1", "::ffff:127.0.0.1"} {
		if _, err := ParseProbePolicy(raw, DefaultProbeMaxTargets); err == nil {
			t.Errorf("ParseProbePolicy(%q) debio fallar (rango prohibido)", raw)
		}
	}
}

func TestParseProbePolicyErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, raw string
		max       int
		wantErr   string // vacio = sin error
	}{
		{"vacia = desactivada", "", 256, ""},
		{"solo comas y espacios", " , ,", 256, ""},
		{"/24 es el minimo", "10.0.0.0/24", 256, ""},
		{"/23 muy grande", "10.0.0.0/23", 1024, "mascara minima /24"},
		{"/16 muy grande", "10.0.0.0/16", 65536, "mascara minima /24"},
		{"ipv6 /64 muy grande", "2001:db8::/64", 256, "mascara minima /120"},
		{"hostname no vale", "router.local", 256, "no es una IP"},
		{"puerto no vale", "10.0.0.1:161", 256, "no es una IP"},
		{"inyeccion", "10.0.0.1;ls", 256, "no es una IP"},
		{"tope de objetivos", "10.0.0.0/24,10.0.1.0/24", 256, "mas de 256 IPs"},
		{"tope subido", "10.0.0.0/24,10.0.1.0/24", 512, ""},
		{"tope 0", "10.0.0.1", 0, "tope"},
		{"IPs sueltas dentro del tope", "10.0.0.1,10.0.0.2", 2, ""},
		{"IPs sueltas sobre el tope", "10.0.0.1,10.0.0.2,10.0.0.3", 2, "mas de 2 IPs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseProbePolicy(tt.raw, tt.max)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("err = %v, want nil", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Errorf("err = %v, want que contenga %q", err, tt.wantErr)
			}
		})
	}
}

func TestProbePolicyDisabledAllowsNothing(t *testing.T) {
	t.Parallel()
	empty, err := ParseProbePolicy("", DefaultProbeMaxTargets)
	if err != nil {
		t.Fatalf("ParseProbePolicy: %v", err)
	}
	var nilPol *ProbePolicy
	for name, p := range map[string]*ProbePolicy{"vacia": empty, "nil": nilPol} {
		if p.Enabled() {
			t.Errorf("%s: Enabled() = true, want false", name)
		}
		if p.Allows(netip.MustParseAddr("192.168.1.10")) {
			t.Errorf("%s: no debe permitir nada", name)
		}
	}
}

func TestLastAddr(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"192.168.1.0/24": "192.168.1.255",
		"10.0.0.128/25":  "10.0.0.255",
		"10.0.0.4/30":    "10.0.0.7",
	}
	for cidr, want := range tests {
		if got := lastAddr(netip.MustParsePrefix(cidr)).String(); got != want {
			t.Errorf("lastAddr(%s) = %s, want %s", cidr, got, want)
		}
	}
}
