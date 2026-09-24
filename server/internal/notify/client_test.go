package notify

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeResolver resuelve nombres a IPs fijas; steps permite cambiar la respuesta en cada llamada (DNS rebinding).
type fakeResolver struct {
	mu    sync.Mutex
	steps [][]netip.Addr // respuesta de la llamada n (la ultima se repite)
	calls int
	err   error
}

func (f *fakeResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	i := min(f.calls, len(f.steps)-1)
	f.calls++
	return f.steps[i], nil
}

func addrs(list ...string) []netip.Addr {
	out := make([]netip.Addr, len(list))
	for i, s := range list {
		out[i] = netip.MustParseAddr(s)
	}
	return out
}

// tlsReceiver levanta un receptor https en 127.0.0.1 (certificado de httptest, valido pa example.com) y devuelve
// la URL con hostname example.com (el resolvedor falso lo lleva a 127.0.0.1), el pool de CAs y el puerto.
func tlsReceiver(t *testing.T, h http.Handler) (rawURL string, roots *x509.CertPool, port string) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	roots = x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	return "https://example.com:" + u.Port() + "/hook?token=SECRET-TOKEN", roots, u.Port()
}

func newTestClient(t *testing.T, allow string, res Resolver, roots *x509.CertPool) *Client {
	t.Helper()
	p, err := ParsePolicy(allow)
	if err != nil {
		t.Fatal(err)
	}
	return NewClient(p, ClientOptions{Resolver: res, RootCAs: roots, Now: func() time.Time { return t0 }})
}

func sendErr(t *testing.T, err error) *SendError {
	t.Helper()
	var se *SendError
	if !errors.As(err, &se) {
		t.Fatalf("err got %v (%T), want *SendError", err, err)
	}
	return se
}

func TestSendDeliversASignedJSONPost(t *testing.T) {
	t.Parallel()
	var got struct {
		method, ctype, ts, sig, ua string
		body                       string
	}
	rawURL, roots, _ := tlsReceiver(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got.method, got.ctype, got.ts, got.sig, got.ua = r.Method, r.Header.Get("Content-Type"), r.Header.Get("X-Argos-Timestamp"), r.Header.Get("X-Argos-Signature"), r.UserAgent()
		got.body = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	c := newTestClient(t, "example.com", &fakeResolver{steps: [][]netip.Addr{addrs("127.0.0.1")}}, roots)

	body := []byte(`{"kind":"test"}`)
	status, err := c.Send(context.Background(), Target{URL: rawURL, HMACSecret: "s3cr3t"}, body)
	if err != nil || status != http.StatusNoContent {
		t.Fatalf("Send got %d, %v", status, err)
	}
	if got.method != "POST" || got.ctype != "application/json" || got.body != string(body) || got.ua != "argos-server" {
		t.Errorf("peticion got %+v", got)
	}
	ts, _ := strconv.ParseInt(got.ts, 10, 64)
	if ts != t0.Unix() {
		t.Errorf("timestamp got %q, want %d", got.ts, t0.Unix())
	}
	if want := "sha256=" + Sign("s3cr3t", ts, body); got.sig != want {
		t.Errorf("firma got %q, want %q", got.sig, want)
	}
	if Sign("otro", ts, body) == Sign("s3cr3t", ts, body) || Sign("s3cr3t", ts+1, body) == Sign("s3cr3t", ts, body) {
		t.Error("la firma debe depender del secreto y del timestamp")
	}
}

func TestSendWithoutSecretDoesNotSign(t *testing.T) {
	t.Parallel()
	var sig string
	rawURL, roots, _ := tlsReceiver(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sig = r.Header.Get("X-Argos-Signature") }))
	c := newTestClient(t, "example.com", &fakeResolver{steps: [][]netip.Addr{addrs("127.0.0.1")}}, roots)
	if _, err := c.Send(context.Background(), Target{URL: rawURL}, []byte("{}")); err != nil || sig != "" {
		t.Errorf("err=%v firma=%q, want nil y vacia", err, sig)
	}
}

func TestSendDoesNotFollowRedirects(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	internal := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	t.Cleanup(internal.Close)
	rawURL, roots, _ := tlsReceiver(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal.URL+"/metadata", http.StatusFound)
	}))
	c := newTestClient(t, "example.com", &fakeResolver{steps: [][]netip.Addr{addrs("127.0.0.1")}}, roots)

	status, err := c.Send(context.Background(), Target{URL: rawURL}, []byte("{}"))
	se := sendErr(t, err)
	if se.Category != CatStatus || status != http.StatusFound || se.HTTPStatus != http.StatusFound {
		t.Errorf("got status=%d %+v, want 302 y categoria status", status, se)
	}
	if hits.Load() != 0 {
		t.Error("el cliente siguio el redirect")
	}
}

func TestSendStatusAndBodyHandling(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		status  int
		body    string
		wantCat string
	}{
		{"200", 200, "ok", ""},
		{"204", 204, "", ""},
		{"500", 500, "internal secret detail", CatStatus},
		{"404", 404, "", CatStatus},
		{"429", 429, "", CatStatus},
		{"cuerpo enorme en 200", 200, strings.Repeat("x", 1<<20), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rawURL, roots, _ := tlsReceiver(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			c := newTestClient(t, "example.com", &fakeResolver{steps: [][]netip.Addr{addrs("127.0.0.1")}}, roots)
			status, err := c.Send(context.Background(), Target{URL: rawURL}, []byte("{}"))
			if tt.wantCat == "" {
				if err != nil {
					t.Fatalf("err got %v, want nil", err)
				}
				return
			}
			se := sendErr(t, err)
			if se.Category != tt.wantCat || status != tt.status {
				t.Errorf("got status=%d %+v", status, se)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Errorf("el error filtra el cuerpo de la respuesta: %v", err)
			}
		})
	}
}

func TestSendBlocksInternalDestinationsWithoutAllowlist(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	rawURL, roots, port := tlsReceiver(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	tests := []struct {
		name string
		url  string
		ips  []netip.Addr
	}{
		{"hostname que resuelve a loopback en 443", "https://example.com/hook", addrs("127.0.0.1")},
		{"resuelve a loopback y puerto no estandar", rawURL, addrs("127.0.0.1")},
		{"IP mezclada: una publica y una privada", "https://example.com/hook", addrs("8.8.8.8", "10.0.0.1")},
		{"metadata", "https://example.com/hook", addrs("169.254.169.254")},
		{"v4 mapeada a loopback", "https://example.com/hook", addrs("::ffff:127.0.0.1")},
		{"IP literal loopback con puerto", "https://127.0.0.1:" + port + "/hook", nil},
	}
	t.Run("casos", func(t *testing.T) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				res := &fakeResolver{steps: [][]netip.Addr{tt.ips}}
				c := newTestClient(t, "", res, roots)
				_, err := c.Send(context.Background(), Target{URL: tt.url}, []byte("{}"))
				se := sendErr(t, err)
				if !se.Blocked || se.Category != CatConnect {
					t.Errorf("got %+v, want bloqueado con categoria connect", se)
				}
			})
		}
	})
	if hits.Load() != 0 {
		t.Error("una peticion llego al destino interno")
	}
}

func TestResolverThatChangesIPIsRevalidatedAtDialTime(t *testing.T) {
	t.Parallel()
	_, roots, _ := tlsReceiver(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	// Al guardar la URL resuelve a una IP publica; al enviar (DNS rebinding) resuelve a loopback.
	res := &fakeResolver{steps: [][]netip.Addr{addrs("93.184.216.34"), addrs("127.0.0.1")}}
	c := newTestClient(t, "", res, roots)

	if err := c.CheckURL(context.Background(), "https://example.com/hook"); err != nil {
		t.Fatalf("CheckURL con IP publica: %v", err)
	}
	_, err := c.Send(context.Background(), Target{URL: "https://example.com/hook"}, []byte("{}"))
	if se := sendErr(t, err); !se.Blocked {
		t.Errorf("Send tras el cambio de IP got %+v, want bloqueado", se)
	}
}

func TestCheckURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		allow   string
		url     string
		ips     []netip.Addr
		dnsErr  error
		wantErr error
	}{
		{"publica", "", "https://example.com/x", addrs("93.184.216.34"), nil, nil},
		{"resuelve a privada", "", "https://example.com/x", addrs("192.168.1.10"), nil, ErrBlockedDestination},
		{"resuelve a privada pero el host esta permitido", "example.com", "https://example.com/x", addrs("192.168.1.10"), nil, nil},
		{"no resuelve", "", "https://example.com/x", nil, errors.New("nxdomain"), ErrURLInvalid},
		{"http", "", "http://example.com/x", addrs("93.184.216.34"), nil, ErrURLInvalid},
		{"literal privada", "", "https://10.0.0.1/x", nil, nil, ErrBlockedDestination},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := &fakeResolver{steps: [][]netip.Addr{tt.ips}, err: tt.dnsErr}
			err := newTestClient(t, tt.allow, res, nil).CheckURL(context.Background(), tt.url)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) {
				t.Errorf("CheckURL err got %v, want %v", err, tt.wantErr)
			}
			if err != nil && strings.Contains(err.Error(), "example.com") {
				t.Errorf("el error repite la URL: %v", err)
			}
		})
	}
}

func TestSendClassifiesFailures(t *testing.T) {
	t.Parallel()
	rawURL, roots, _ := tlsReceiver(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	loop := &fakeResolver{steps: [][]netip.Addr{addrs("127.0.0.1")}}

	t.Run("dns", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t, "example.com", &fakeResolver{err: errors.New("nxdomain")}, roots)
		_, err := c.Send(context.Background(), Target{URL: rawURL}, []byte("{}"))
		if se := sendErr(t, err); se.Category != CatDNS {
			t.Errorf("got %+v", se)
		}
	})
	t.Run("tls con certificado no confiable", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t, "example.com", loop, nil) // sin la CA del receptor
		_, err := c.Send(context.Background(), Target{URL: rawURL}, []byte("{}"))
		if se := sendErr(t, err); se.Category != CatTLS {
			t.Errorf("got %+v", se)
		}
	})
	t.Run("connect con puerto cerrado", func(t *testing.T) {
		t.Parallel()
		l := httptest.NewServer(nil)
		lu, err := url.Parse(l.URL)
		if err != nil {
			t.Fatal(err)
		}
		l.Close()
		c := newTestClient(t, "example.com", loop, roots)
		_, err = c.Send(context.Background(), Target{URL: "https://example.com:" + lu.Port() + "/x"}, []byte("{}"))
		if se := sendErr(t, err); se.Category != CatConnect || se.Blocked {
			t.Errorf("got %+v", se)
		}
	})
	t.Run("timeout por contexto", func(t *testing.T) {
		t.Parallel()
		release := make(chan struct{})
		slowURL, slowRoots, _ := tlsReceiver(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
		t.Cleanup(func() { close(release) })
		c := newTestClient(t, "example.com", loop, slowRoots)
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		_, err := c.Send(ctx, Target{URL: slowURL}, []byte("{}"))
		if se := sendErr(t, err); se.Category != CatTimeout {
			t.Errorf("got %+v", se)
		}
	})
}

func TestSendErrorMessagesNeverCarryTheURL(t *testing.T) {
	t.Parallel()
	for _, se := range []*SendError{{Category: CatDNS}, {Category: CatTLS}, {Category: CatConnect, Blocked: true}, {Category: CatStatus, HTTPStatus: 502}} {
		if msg := se.Error(); len(msg) > 100 || strings.Contains(msg, "://") {
			t.Errorf("mensaje sospechoso: %q", msg)
		}
	}
}
