package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func mustPrefixes(t *testing.T, cidrs ...string) []netip.Prefix {
	t.Helper()
	out := make([]netip.Prefix, 0, len(cidrs))
	for _, c := range cidrs {
		p, err := netip.ParsePrefix(c)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

func TestLimiter(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l := newLimiter(3, 30*time.Second) // 3 de rafaga, 1 token cada 10 s

	for i := range 3 {
		if ok, _ := l.allow("a", now); !ok {
			t.Fatalf("rafaga %d rechazada", i+1)
		}
	}
	ok, wait := l.allow("a", now)
	if ok || wait != 10*time.Second {
		t.Errorf("4to intento got (%v, %v), want (false, 10s)", ok, wait)
	}
	if ok, _ := l.allow("b", now); !ok {
		t.Error("otra clave tiene su propio bucket")
	}
	if ok, _ := l.allow("a", now.Add(9*time.Second)); ok {
		t.Error("a los 9 s aun no hay token")
	}
	if ok, _ := l.allow("a", now.Add(10*time.Second)); !ok {
		t.Error("a los 10 s debe haber un token")
	}
	// Un bucket inactivo se recupera al maximo (nunca acumula mas que la rafaga).
	later := now.Add(time.Hour)
	for i := range 3 {
		if ok, _ := l.allow("a", later); !ok {
			t.Fatalf("tras la espera, intento %d rechazado", i+1)
		}
	}
	if ok, _ := l.allow("a", later); ok {
		t.Error("la rafaga no puede superar el maximo")
	}
}

func TestLimiterSweepsRecoveredBuckets(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l := newLimiter(2, time.Minute)
	for _, k := range []string{"a", "b", "c"} {
		l.allow(k, now)
	}
	l.allow("d", now.Add(2*time.Minute)) // dispara el barrido: a, b y c ya se recuperaron
	if got := len(l.buckets); got != 1 {
		t.Errorf("buckets tras el barrido got %d, want 1", got)
	}
}

func TestIPKey(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		{"192.0.2.1", "192.0.2.1"},
		{"::ffff:192.0.2.1", "192.0.2.1"},
		{"2001:db8:1:2:aaaa:bbbb:cccc:dddd", "2001:db8:1:2::/64"},
		{"2001:db8:1:2:1111:2222:3333:4444", "2001:db8:1:2::/64"}, // mismo /64, misma clave
	}
	for _, tt := range tests {
		if got := ipKey(netip.MustParseAddr(tt.in)); got != tt.want {
			t.Errorf("ipKey(%s) got %q, want %q", tt.in, got, tt.want)
		}
	}
	if got := ipKey(netip.Addr{}); got != "" {
		t.Errorf("ipKey(invalida) got %q, want vacio", got)
	}
}

func TestClientIP(t *testing.T) {
	t.Parallel()
	trusted := mustPrefixes(t, "10.0.0.0/8", "192.168.1.5/32")
	tests := []struct {
		name    string
		trusted []netip.Prefix
		remote  string
		xff     []string
		want    string
	}{
		{"sin proxies usa RemoteAddr", nil, "203.0.113.9:5000", []string{"1.2.3.4"}, "203.0.113.9"},
		{"remoto no confiable ignora XFF", trusted, "203.0.113.9:5000", []string{"1.2.3.4"}, "203.0.113.9"},
		{"proxy confiable usa XFF", trusted, "10.0.0.1:443", []string{"203.0.113.7"}, "203.0.113.7"},
		{"cadena de proxies", trusted, "10.0.0.1:443", []string{"203.0.113.7, 10.0.0.2"}, "203.0.113.7"},
		{"falsificacion a la izquierda", trusted, "10.0.0.1:443", []string{"1.2.3.4, 203.0.113.7"}, "203.0.113.7"},
		{"varias cabeceras XFF", trusted, "10.0.0.1:443", []string{"1.2.3.4", "203.0.113.7"}, "203.0.113.7"},
		{"XFF malformado cae al proxy", trusted, "10.0.0.1:443", []string{"basura"}, "10.0.0.1"},
		{"sin XFF cae al proxy", trusted, "10.0.0.1:443", nil, "10.0.0.1"},
		{"ipv4 mapeada en ipv6", trusted, "[::ffff:10.0.0.1]:443", []string{"203.0.113.7"}, "203.0.113.7"},
		{"host unico confiable", trusted, "192.168.1.5:1", []string{"198.51.100.1"}, "198.51.100.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := &API{opts: Options{TrustedProxies: tt.trusted}}
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tt.remote
			for _, x := range tt.xff {
				r.Header.Add("X-Forwarded-For", x)
			}
			if got := a.clientIP(r).String(); got != tt.want {
				t.Errorf("clientIP got %s, want %s", got, tt.want)
			}
		})
	}

	a := &API{}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "basura"
	if got := a.clientIP(r); got.IsValid() {
		t.Errorf("RemoteAddr ilegible debe dar direccion invalida, got %v", got)
	}
}

func TestIsUUID(t *testing.T) {
	t.Parallel()
	valid := []string{"00000000-0000-4000-8000-000000000000", "A774E4A8-dc27-4f96-b786-6784f7ce20a6"}
	invalid := []string{
		"", "no-uuid", "a774e4a8dc274f96b7866784f7ce20a6",
		"a774e4a8-dc27-4f96-b786-6784f7ce20a", "a774e4a8-dc27-4f96-b786-6784f7ce20ag",
	}
	for _, s := range valid {
		if !isUUID(s) {
			t.Errorf("isUUID(%q) = false, want true", s)
		}
	}
	for _, s := range invalid {
		if isUUID(s) {
			t.Errorf("isUUID(%q) = true, want false", s)
		}
	}
}

func TestTruncRunes(t *testing.T) {
	t.Parallel()
	if got := truncRunes(strings.Repeat("ñ", 10), 4); got != "ññññ" {
		t.Errorf("got %q, want ññññ (no debe partir UTF-8)", got)
	}
	if got := truncRunes("ok\xffbad", 100); got != "ok�bad" {
		t.Errorf("bytes invalidos got %q", got)
	}
}

func TestMarshalDetailKeepsOnlyWhitelistedKeys(t *testing.T) {
	t.Parallel()
	got := string(marshalDetail(map[string]any{"reason": "bad_password", "password": "x", "token": "y", "locked": true}))
	if got != `{"locked":true,"reason":"bad_password"}` {
		t.Errorf("detalle got %s", got)
	}
	if marshalDetail(map[string]any{"password": "x"}) != nil || marshalDetail(nil) != nil {
		t.Error("sin claves permitidas el detalle debe ser nil")
	}
}

func TestNormalizeHost(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"localhost:8080": "localhost", "LOCALHOST": "localhost", "[::1]:8080": "::1", "[::1]": "::1",
		"argos.example.com.": "argos.example.com", "127.0.0.1": "127.0.0.1",
	}
	for in, want := range tests {
		if got := normalizeHost(in); got != want {
			t.Errorf("normalizeHost(%q) got %q, want %q", in, got, want)
		}
	}
}

func TestRetryAfterSeconds(t *testing.T) {
	t.Parallel()
	tests := map[time.Duration]string{time.Millisecond: "1", time.Second: "1", 1500 * time.Millisecond: "2", 30 * time.Second: "30"}
	for d, want := range tests {
		if got := retryAfterSeconds(d); got != want {
			t.Errorf("retryAfterSeconds(%v) got %q, want %q", d, got, want)
		}
	}
}

func TestIsSafeMethod(t *testing.T) {
	t.Parallel()
	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		if !isSafeMethod(m) {
			t.Errorf("%s debe ser seguro", m)
		}
	}
	for _, m := range []string{http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete, "FOO"} {
		if isSafeMethod(m) {
			t.Errorf("%s no debe ser seguro", m)
		}
	}
}
