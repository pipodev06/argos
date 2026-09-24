package transport

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func get(t *testing.T, c *http.Client, url string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c.Do(req) //nolint:bodyclose // el llamador cierra cuando no hay error
}

func writeCA(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ca.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPrivateCA(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	// Sin la CA privada el certificado autofirmado se rechaza (nunca InsecureSkipVerify).
	plain, err := NewClient("", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if resp, err := get(t, plain, srv.URL); err == nil {
		_ = resp.Body.Close()
		t.Fatal("se esperaba error de certificado desconocido sin ARGOS_CA_FILE")
	}

	withCA, err := NewClient(writeCA(t, srv), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := get(t, withCA, srv.URL)
	if err != nil {
		t.Fatalf("con la CA propia deberia conectar: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status got %d", resp.StatusCode)
	}
}

func TestInvalidCAFile(t *testing.T) {
	t.Parallel()

	bad := filepath.Join(t.TempDir(), "bad.pem")
	if err := os.WriteFile(bad, []byte("no es un pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewClient(bad, time.Second); err == nil {
		t.Error("PEM invalido: se esperaba error")
	}
	if _, err := NewClient(filepath.Join(t.TempDir(), "no-existe.pem"), time.Second); err == nil {
		t.Error("archivo inexistente: se esperaba error")
	}
}

func TestDoesNotFollowRedirects(t *testing.T) {
	t.Parallel()

	var hits int
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirector.Close()

	c, err := NewClient("", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := get(t, c, redirector.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound || hits != 0 {
		t.Errorf("status=%d hits=%d, want 302 y 0 (no se sigue el redirect: el bearer no debe viajar a otro destino)", resp.StatusCode, hits)
	}
}
