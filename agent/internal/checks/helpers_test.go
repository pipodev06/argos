package checks

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"
)

// fakeProber es un ServiceProber inyectable: cuenta llamadas y concurrencia.
type fakeProber struct {
	fn    func(call int) (bool, error)
	delay time.Duration

	mu      sync.Mutex
	calls   int
	cur     int
	maxSeen int
}

func newFakeProber(fn func(call int) (bool, error), delay time.Duration) *fakeProber {
	return &fakeProber{fn: fn, delay: delay}
}

func (f *fakeProber) Probe(ctx context.Context, _ string) (bool, error) {
	f.mu.Lock()
	f.calls++
	call := f.calls
	f.cur++
	f.maxSeen = max(f.maxSeen, f.cur)
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.cur--
		f.mu.Unlock()
	}()
	select {
	case <-time.After(f.delay):
	case <-ctx.Done():
		return false, ctx.Err()
	}
	return f.fn(call)
}

func (f *fakeProber) stats() (calls, maxSeen int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.maxSeen
}

func fixedResolver(ips ...string) func(context.Context, string) ([]netip.Addr, error) {
	return func(context.Context, string) ([]netip.Addr, error) {
		out := make([]netip.Addr, len(ips))
		for i, s := range ips {
			out[i] = netip.MustParseAddr(s)
		}
		return out, nil
	}
}

func noOwnIPs() []netip.Addr { return nil }

func newTestRunner(t *testing.T, o Options) *Runner {
	t.Helper()
	if o.OwnIPs == nil {
		o.OwnIPs = noOwnIPs
	}
	r := NewRunner(o)
	t.Cleanup(r.Close)
	return r
}

func portOf(t *testing.T, a net.Addr) int {
	t.Helper()
	ta, ok := a.(*net.TCPAddr)
	if !ok {
		t.Fatalf("addr %T no es TCP", a)
	}
	return ta.Port
}

// tlsListener sirve un certificado autofirmado para 127.0.0.1 con la vigencia dada.
// Cada conexion aceptada hace el handshake y reporta cuantos bytes de aplicacion llegaron.
func tlsListener(t *testing.T, notBefore, notAfter time.Time) (port int, cert *x509.Certificate, received <-chan int) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generando clave: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "argos-test"},
		NotBefore: notBefore, NotAfter: notAfter,
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:        true, BasicConstraintsValid: true,
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creando certificado: %v", err)
	}
	cert, err = x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("leyendo certificado: %v", err)
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		t.Fatalf("escuchando: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	ch := make(chan int, 8)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				tc, ok := conn.(*tls.Conn)
				if !ok || tc.Handshake() != nil {
					return
				}
				buf := make([]byte, 64)
				_ = tc.SetReadDeadline(time.Now().Add(2 * time.Second))
				n, _ := tc.Read(buf)
				ch <- n
			}()
		}
	}()
	return portOf(t, ln.Addr()), cert, ch
}
