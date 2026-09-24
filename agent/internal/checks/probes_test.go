package checks

import (
	"context"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTCP(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	openPort := portOf(t, ln.Addr())
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := portOf(t, closed.Addr())
	_ = closed.Close()

	r := newTestRunner(t, Options{})
	tests := []struct {
		name       string
		host       string
		port       int
		wantStatus string
		wantDetail string
	}{
		{"abierto", "127.0.0.1", openPort, StatusUp, DetailOK},
		{"rechazado", "127.0.0.1", closedPort, StatusDown, DetailRefused},
		{"privada sin allowlist", "10.0.0.5", 22, StatusUnknown, DetailBlocked},
		{"publica sin allowlist", "8.8.8.8", 53, StatusUnknown, DetailBlocked},
		{"puerto invalido", "127.0.0.1", 0, StatusUnknown, DetailUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := r.probe(t.Context(), Check{Type: TypeTCP, TargetHost: tt.host, TargetPort: tt.port, TimeoutMS: 1000})
			if got.status != tt.wantStatus || got.detail != tt.wantDetail {
				t.Errorf("got %s/%s, want %s/%s", got.status, got.detail, tt.wantStatus, tt.wantDetail)
			}
		})
	}
}

func TestLatencyThresholds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		ms         int64
		warn, crit int
		want       string
	}{
		{"sin umbrales", 9999, 0, 0, StatusUp},
		{"bajo warn", 50, 100, 500, StatusUp},
		{"en warn", 100, 100, 500, StatusWarn},
		{"entre warn y crit", 499, 100, 500, StatusWarn},
		{"en crit", 500, 100, 500, StatusDown},
		{"solo crit", 600, 0, 500, StatusDown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := latencyOutcome(tt.ms, tt.warn, tt.crit); got.status != tt.want || got.detail != DetailOK {
				t.Errorf("got %s/%s, want %s/ok", got.status, got.detail, tt.want)
			}
		})
	}
}

func TestDNSChangeIsRevalidated(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	c := Check{Type: TypeTCP, TargetHost: "svc.test", TargetPort: portOf(t, ln.Addr()), TimeoutMS: 1000}

	var calls atomic.Int32
	answers := [][]string{{"127.0.0.1"}, {"10.0.0.5"}, {"127.0.0.1", "10.0.0.5"}}
	r := newTestRunner(t, Options{Resolve: func(ctx context.Context, host string) ([]netip.Addr, error) {
		n := int(calls.Add(1)) - 1
		return fixedResolver(answers[n]...)(ctx, host)
	}})
	want := []struct{ status, detail string }{
		{StatusUp, DetailOK},           // resuelve a loopback
		{StatusUnknown, DetailBlocked}, // el DNS cambia a una IP privada
		{StatusUnknown, DetailBlocked}, // respuesta mixta: se validan TODAS
	}
	for i, w := range want {
		got := r.probe(t.Context(), c)
		if got.status != w.status || got.detail != w.detail {
			t.Errorf("intento %d: got %s/%s, want %s/%s", i, got.status, got.detail, w.status, w.detail)
		}
	}
}

func TestBlockedIsNotRetried(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	r := newTestRunner(t, Options{Resolve: func(context.Context, string) ([]netip.Addr, error) {
		calls.Add(1)
		return []netip.Addr{netip.MustParseAddr("10.0.0.5")}, nil
	}})
	got := r.attempts(t.Context(), Check{Type: TypeTCP, TargetHost: "svc.test", TargetPort: 22, Retries: 3, TimeoutMS: 500})
	if got.detail != DetailBlocked || calls.Load() != 1 {
		t.Errorf("got %s con %d resoluciones, want blocked con 1", got.detail, calls.Load())
	}
}

func TestHTTP(t *testing.T) {
	t.Parallel()
	var otherHits atomic.Int32
	var gotHeaders atomic.Value
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {
		gotHeaders.Store(r.Header.Clone())
		_, _ = w.Write([]byte("hello needle"))
	})
	mux.HandleFunc("/redir", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/other", http.StatusFound) })
	mux.HandleFunc("/other", func(http.ResponseWriter, *http.Request) { otherHits.Add(1) })
	mux.HandleFunc("/big", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("a", MaxBodyBytes)))
		_, _ = w.Write([]byte("needle"))
	})
	mux.HandleFunc("/slow", func(http.ResponseWriter, *http.Request) { time.Sleep(300 * time.Millisecond) })
	mux.HandleFunc("/fail", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	r := newTestRunner(t, Options{})

	tests := []struct {
		name       string
		c          Check
		wantStatus string
		wantDetail string
	}{
		{"ok", Check{URL: srv.URL + "/ok", ExpectStatus: "200"}, StatusUp, DetailOK},
		{"texto presente", Check{URL: srv.URL + "/ok", ExpectText: "needle"}, StatusUp, DetailOK},
		{"texto ausente", Check{URL: srv.URL + "/ok", ExpectText: "zzz"}, StatusDown, DetailTextMismatch},
		{"texto mas alla de 64 KiB", Check{URL: srv.URL + "/big", ExpectText: "needle"}, StatusDown, DetailTextMismatch},
		{"status distinto", Check{URL: srv.URL + "/fail", ExpectStatus: "200-399"}, StatusDown, DetailStatusMismatch},
		{"redirect no se sigue (200 exacto)", Check{URL: srv.URL + "/redir", ExpectStatus: "200"}, StatusDown, DetailStatusMismatch},
		{"redirect aceptado como 3xx", Check{URL: srv.URL + "/redir", ExpectStatus: "200-399"}, StatusUp, DetailOK},
		{"timeout", Check{URL: srv.URL + "/slow", TimeoutMS: 200}, StatusDown, DetailTimeout},
		{"latencia warn", Check{URL: srv.URL + "/slow", WarnMS: 100, CritMS: 5000, ExpectStatus: "200"}, StatusWarn, DetailOK},
		{"destino ajeno bloqueado", Check{URL: "http://10.0.0.5:8080/"}, StatusUnknown, DetailBlocked},
		{"credenciales en la url", Check{URL: "http://u:p@127.0.0.1/"}, StatusUnknown, DetailUnsupported},
		{"esquema no http", Check{URL: "ftp://127.0.0.1/"}, StatusUnknown, DetailUnsupported},
		{"expect_status invalido", Check{URL: srv.URL + "/ok", ExpectStatus: "abc"}, StatusUnknown, DetailUnsupported},
		{"cabecera fuera de la allowlist", Check{URL: srv.URL + "/ok", HeaderName: "Cookie", Secret: "x"}, StatusUnknown, DetailUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.c.Type = TypeHTTP
			if tt.c.TimeoutMS == 0 {
				tt.c.TimeoutMS = 2000
			}
			got := r.probe(t.Context(), tt.c)
			if got.status != tt.wantStatus || got.detail != tt.wantDetail {
				t.Errorf("got %s/%s, want %s/%s", got.status, got.detail, tt.wantStatus, tt.wantDetail)
			}
		})
	}
	if n := otherHits.Load(); n != 0 {
		t.Errorf("el redirect se siguio: %d peticiones a /other", n)
	}

	t.Run("cabecera secreta y sin cabeceras de argos", func(t *testing.T) {
		got := r.probe(t.Context(), Check{Type: TypeHTTP, URL: srv.URL + "/ok", HeaderName: "x-api-key", Secret: "s3cr3t", TimeoutMS: 2000})
		if got.status != StatusUp {
			t.Fatalf("got %s/%s, want up", got.status, got.detail)
		}
		h, ok := gotHeaders.Load().(http.Header)
		if !ok {
			t.Fatal("el servidor no vio la peticion")
		}
		if h.Get("X-Api-Key") != "s3cr3t" || h.Get("Authorization") != "" {
			t.Errorf("cabeceras inesperadas: %v", h)
		}
	})
}

func TestHTTPSVerifiesTLS(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(srv.Close)
	c := Check{Type: TypeHTTP, URL: srv.URL, TimeoutMS: 2000}

	untrusted := newTestRunner(t, Options{})
	if got := untrusted.probe(t.Context(), c); got.status != StatusDown || got.detail != DetailTLS {
		t.Errorf("sin CA: got %s/%s, want down/tls", got.status, got.detail)
	}
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	trusted := newTestRunner(t, Options{Roots: pool})
	if got := trusted.probe(t.Context(), c); got.status != StatusUp {
		t.Errorf("con CA: got %s/%s, want up", got.status, got.detail)
	}
}

func TestTLSCert(t *testing.T) {
	t.Parallel()
	now := time.Now()
	day := 24 * time.Hour
	tests := []struct {
		name        string
		notAfter    time.Time
		wantStatus  string
		wantDetail  string
		wantDaysMin float64
		wantDaysMax float64
	}{
		{"valido", now.Add(30 * day), StatusUp, DetailOK, 29.5, 30.1},
		{"por vencer (warn)", now.Add(5 * day), StatusWarn, DetailOK, 4.5, 5.1},
		{"critico", now.Add(2 * day), StatusDown, DetailOK, 1.5, 2.1},
		{"vencido", now.Add(-day), StatusDown, DetailExpired, -1.1, -0.9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			port, cert, received := tlsListener(t, now.Add(-60*day), tt.notAfter)
			r := newTestRunner(t, Options{})
			c := Check{Type: TypeTLSCert, TargetHost: "127.0.0.1", TargetPort: port, TimeoutMS: 3000, WarnDays: 10, CritDays: 3}
			got := r.probe(t.Context(), c)
			if got.status != tt.wantStatus || got.detail != tt.wantDetail {
				t.Fatalf("got %s/%s, want %s/%s", got.status, got.detail, tt.wantStatus, tt.wantDetail)
			}
			if got.value == nil || *got.value < tt.wantDaysMin || *got.value > tt.wantDaysMax {
				t.Errorf("value = %v, want entre %v y %v", got.value, tt.wantDaysMin, tt.wantDaysMax)
			}
			if got.trusted == nil || *got.trusted {
				t.Errorf("trusted = %v, want false (autofirmado)", got.trusted)
			}
			select {
			case n := <-received:
				if n != 0 {
					t.Errorf("el servidor recibio %d bytes de aplicacion, want 0", n)
				}
			case <-time.After(3 * time.Second):
				t.Error("el servidor no vio cerrarse la conexion")
			}

			// Con la CA en el pool la cadena valida (salvo que este vencido).
			pool := x509.NewCertPool()
			pool.AddCert(cert)
			withCA := newTestRunner(t, Options{Roots: pool})
			got = withCA.probe(t.Context(), c)
			wantTrusted := tt.notAfter.After(now)
			if got.trusted == nil || *got.trusted != wantTrusted {
				t.Errorf("con CA: trusted = %v, want %v", got.trusted, wantTrusted)
			}
		})
	}
}

func TestTLSCertErrors(t *testing.T) {
	t.Parallel()
	plain, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = plain.Close() })
	go func() {
		for {
			conn, err := plain.Accept()
			if err != nil {
				return
			}
			_, _ = conn.Write([]byte("no soy tls\r\n"))
			_ = conn.Close()
		}
	}()
	r := newTestRunner(t, Options{})
	c := Check{Type: TypeTLSCert, TargetHost: "127.0.0.1", TargetPort: portOf(t, plain.Addr()), TimeoutMS: 2000, WarnDays: 10, CritDays: 3}
	if got := r.probe(t.Context(), c); got.status != StatusDown || got.detail != DetailTLS {
		t.Errorf("sin TLS: got %s/%s, want down/tls", got.status, got.detail)
	}
	c.TargetHost, c.TargetPort = "10.0.0.5", 443
	if got := r.probe(t.Context(), c); got.detail != DetailBlocked {
		t.Errorf("destino ajeno: got %s, want blocked", got.detail)
	}
}

func TestUnknownTypeAndServiceMapping(t *testing.T) {
	t.Parallel()
	svc := func(name string) Check { return Check{Type: TypeService, ServiceName: name} }
	tests := []struct {
		name       string
		c          Check
		up         bool
		err        error
		wantStatus string
		wantDetail string
	}{
		{"tipo desconocido", Check{Type: "ping"}, false, nil, StatusUnknown, DetailUnsupported},
		{"activo", svc("nginx"), true, nil, StatusUp, DetailOK},
		{"detenido", svc("nginx"), false, nil, StatusDown, DetailStatusMismatch},
		{"no existe", svc("nginx"), false, ErrServiceNotFound, StatusDown, DetailNotFound},
		{"denegado", svc("nginx"), false, ErrServiceDenied, StatusUnknown, DetailDenied},
		{"no soportado", svc("nginx"), false, ErrServiceUnsupported, StatusUnknown, DetailUnsupported},
		{"nombre invalido", svc("-rf; ls"), true, nil, StatusUnknown, DetailUnsupported},
		{"nombre con guion inicial", svc("--now"), true, nil, StatusUnknown, DetailUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newTestRunner(t, Options{Service: newFakeProber(func(int) (bool, error) { return tt.up, tt.err }, 0)})
			got := r.probe(t.Context(), tt.c)
			if got.status != tt.wantStatus || got.detail != tt.wantDetail {
				t.Errorf("got %s/%s, want %s/%s", got.status, got.detail, tt.wantStatus, tt.wantDetail)
			}
		})
	}
}
