package notify

import (
	"errors"
	"net/netip"
	"strings"
	"testing"
)

func TestControlBlocksInternalAddresses(t *testing.T) {
	t.Parallel()
	strict := &Policy{}
	tests := []struct {
		name    string
		address string
		blocked bool
	}{
		{"loopback v4", "127.0.0.1:443", true},
		{"loopback v4 otro", "127.5.5.5:443", true},
		{"loopback v6", "[::1]:443", true},
		{"v4 mapeado a loopback", "[::ffff:127.0.0.1]:443", true},
		{"v4 mapeado a metadata", "[::ffff:169.254.169.254]:443", true},
		{"v4 mapeado a privada", "[::ffff:10.0.0.5]:443", true},
		{"metadata de nube", "169.254.169.254:443", true},
		{"link-local v4", "169.254.1.1:443", true},
		{"link-local v6", "[fe80::1]:443", true},
		{"privada 10/8", "10.1.2.3:443", true},
		{"privada 172.16/12", "172.20.0.1:443", true},
		{"privada 192.168/16", "192.168.1.1:443", true},
		{"ULA fd00::/8", "[fd00::1]:443", true},
		{"ULA fc00::/7", "[fc12::1]:443", true},
		{"metadata AWS v6", "[fd00:ec2::254]:443", true},
		{"CGNAT", "100.64.0.1:443", true},
		{"CGNAT borde alto", "100.127.255.254:443", true},
		{"no especificada v4", "0.0.0.0:443", true},
		{"no especificada v6", "[::]:443", true},
		{"multicast v4", "224.0.0.1:443", true},
		{"multicast v6", "[ff02::1]:443", true},
		{"broadcast", "255.255.255.255:443", true},
		{"reservado 240/4", "240.0.0.1:443", true},
		{"NAT64", "[64:ff9b::7f00:1]:443", true},
		{"NAT64 con v4 publica", "[64:ff9b::808:808]:443", true},
		{"6to4", "[2002:7f00:1::1]:443", true},
		{"Teredo", "[2001:0:4136:e378:8000:63bf:3fff:fdd2]:443", true},
		{"documentacion", "192.0.2.10:443", true},
		{"con zona", "[fe80::1%25eth0]:443", true},
		{"sin puerto", "8.8.8.8", true},
		{"no es una IP", "example.com:443", true},
		{"publica v4", "8.8.8.8:443", false},
		{"publica v4 cerca de CGNAT", "100.128.0.1:443", false},
		{"publica v6", "[2606:4700:4700::1111]:443", false},
		{"puerto no estandar", "8.8.8.8:8443", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := strict.control(false)("tcp", tt.address, nil)
			if got := err != nil; got != tt.blocked {
				t.Errorf("control(%q) bloqueado got %v (err %v), want %v", tt.address, got, err, tt.blocked)
			}
			if err != nil && !errors.Is(err, ErrBlockedDestination) {
				t.Errorf("control(%q) err got %v, want ErrBlockedDestination", tt.address, err)
			}
		})
	}
}

func TestAllowlistOverridesBlockedRanges(t *testing.T) {
	t.Parallel()
	p, err := ParsePolicy("hooks.internal, 10.9.0.0/16 ,192.168.1.50")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		hostOK  bool
		address string
		blocked bool
	}{
		{"host permitido llega a privada y otro puerto", true, "10.1.1.1:8443", false},
		{"IP en CIDR permitido", false, "10.9.4.4:443", false},
		{"IP en CIDR permitido, otro puerto", false, "10.9.4.4:8443", false},
		{"IP suelta permitida", false, "192.168.1.50:443", false},
		{"otra IP privada sigue bloqueada", false, "10.8.0.1:443", true},
		{"loopback no esta en la allowlist", false, "127.0.0.1:443", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := p.control(tt.hostOK)("tcp", tt.address, nil)
			if got := err != nil; got != tt.blocked {
				t.Errorf("control(%q, hostOK=%v) bloqueado got %v, want %v", tt.address, tt.hostOK, got, tt.blocked)
			}
		})
	}
	if !p.hostAllowed("HOOKS.internal.") || p.hostAllowed("evil.hooks.internal") {
		t.Error("la allowlist de hostnames debe ser exacta (sin distinguir mayusculas ni punto final)")
	}
}

func TestParsePolicyRejectsGarbage(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"http://x", "bad host", "*.example.com", "a/b"} {
		if _, err := ParsePolicy(raw); err == nil {
			t.Errorf("ParsePolicy(%q) debio fallar", raw)
		}
	}
	if p, err := ParsePolicy(""); err != nil || len(p.hosts)+len(p.prefixes) != 0 {
		t.Errorf("ParsePolicy vacia got %+v, %v", p, err)
	}
}

func TestParseTarget(t *testing.T) {
	t.Parallel()
	p, err := ParsePolicy("hooks.internal")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"https normal", "https://hooks.example.com/services/T000/B000/xyz", false},
		{"puerto 443 explicito", "https://hooks.example.com:443/x", false},
		{"http", "http://hooks.example.com/x", true},
		{"otro esquema", "ftp://hooks.example.com/x", true},
		{"sin esquema", "hooks.example.com/x", true},
		{"puerto no estandar", "https://hooks.example.com:8443/x", true},
		{"puerto no estandar en host permitido", "https://hooks.internal:8443/x", false},
		{"credenciales", "https://user:pass@hooks.example.com/x", true},
		{"fragmento", "https://hooks.example.com/x#f", true},
		{"IP literal loopback", "https://127.0.0.1/x", true},
		{"IP literal metadata", "https://169.254.169.254/latest", true},
		{"IPv6 literal mapeada", "https://[::ffff:7f00:1]/x", true},
		{"IPv6 loopback", "https://[::1]/x", true},
		{"IP literal publica", "https://8.8.8.8/x", false},
		{"CRLF en el host", "https://hooks.example.com%0d%0aX-Injected:1/x", true},
		{"CRLF crudo", "https://hooks.example.com/x\r\nHost: evil", true},
		{"espacio", "https://hooks.example.com/a b", true},
		{"vacia", "", true},
		{"demasiado larga", "https://h.example.com/" + strings.Repeat("a", 3000), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := p.ParseTarget(tt.raw)
			if got := err != nil; got != tt.wantErr {
				t.Errorf("ParseTarget(%q) err got %v, want error=%v", tt.raw, err, tt.wantErr)
			}
			if err != nil && strings.Contains(err.Error(), "hooks.example.com") {
				t.Errorf("el error no debe repetir la URL: %v", err)
			}
		})
	}
}

func TestBlockedIPUnmapsIPv4(t *testing.T) {
	t.Parallel()
	mapped := netip.MustParseAddr("::ffff:192.168.0.1")
	if !blockedIP(mapped) {
		t.Error("::ffff:192.168.0.1 debe estar bloqueada (se desenvuelve a IPv4 privada)")
	}
	if blockedIP(netip.MustParseAddr("::ffff:8.8.8.8")) {
		t.Error("::ffff:8.8.8.8 es publica")
	}
}
