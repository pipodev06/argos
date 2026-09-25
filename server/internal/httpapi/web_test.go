package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testIndex = "<!doctype html><title>argos</title>"

// newWebDir crea parent/web (index.html + assets/app-1.js) y parent/secret.txt fuera del directorio.
func newWebDir(t *testing.T) (parent, web string) {
	t.Helper()
	parent = t.TempDir()
	web = filepath.Join(parent, "web")
	if err := os.MkdirAll(filepath.Join(web, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		filepath.Join(web, "index.html"):         testIndex,
		filepath.Join(web, "favicon.svg"):        "<svg/>",
		filepath.Join(web, "assets", "app-1.js"): "console.log(1)",
		filepath.Join(parent, "secret.txt"):      "TOP-SECRET",
	}
	for p, c := range files {
		if err := os.WriteFile(p, []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return parent, web
}

func newWebAPI(t *testing.T, web string) http.Handler {
	t.Helper()
	root, err := OpenWebDir(web)
	if err != nil {
		t.Fatalf("OpenWebDir: %v", err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Errorf("cerrando root: %v", err)
		}
	})
	h, _ := newDistAPI(t, Options{Web: root.FS()})
	return h
}

func TestWebServing(t *testing.T) {
	t.Parallel()
	_, web := newWebDir(t)
	h := newWebAPI(t, web)
	tests := []struct {
		name, method, path string
		status             int
		body, cache        string
	}{
		{"raiz", "GET", "/", 200, testIndex, "no-cache"},
		{"index directo", "GET", "/index.html", 200, testIndex, "no-cache"},
		{"ruta del SPA", "GET", "/hosts/00000000-0000-0000-0000-000000000000", 200, testIndex, "no-cache"},
		{"asset", "GET", "/assets/app-1.js", 200, "console.log(1)", "public, max-age=31536000, immutable"},
		{"archivo suelto", "GET", "/favicon.svg", 200, "<svg/>", "no-cache"},
		{"asset inexistente", "GET", "/assets/nada.js", 404, "", ""},
		{"directorio sin listado", "GET", "/assets/", 404, "", ""},
		{"HEAD raiz", "HEAD", "/", 200, "", "no-cache"},
		{"HEAD asset", "HEAD", "/assets/app-1.js", 200, "", "public, max-age=31536000, immutable"},
		{"traversal (el mux redirige a la ruta limpia)", "GET", "/../secret.txt", 307, "", ""},
		{"traversal en assets (redirect)", "GET", "/assets/../../secret.txt", 307, "", ""},
		{"traversal codificado", "GET", "/..%2fsecret.txt", 200, testIndex, "no-cache"},
		{"traversal doble codificado", "GET", "/%2e%2e/secret.txt", 200, testIndex, "no-cache"},
		{"traversal backslash", "GET", `/..%5csecret.txt`, 200, testIndex, "no-cache"},
		{"traversal en assets codificado", "GET", "/assets/..%2f..%2fsecret.txt", 404, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := get(t, h, tt.method, tt.path)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tt.status, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "TOP-SECRET") {
				t.Fatal("filtro un archivo fuera del directorio")
			}
			if tt.status != 200 {
				return
			}
			if tt.method == "GET" && rec.Body.String() != tt.body {
				t.Errorf("body = %q, want %q", rec.Body.String(), tt.body)
			}
			if got := rec.Header().Get("Cache-Control"); got != tt.cache {
				t.Errorf("Cache-Control = %q, want %q", got, tt.cache)
			}
		})
	}
}

func TestWebHeaders(t *testing.T) {
	t.Parallel()
	_, web := newWebDir(t)
	h := newWebAPI(t, web)
	for _, p := range []string{"/", "/assets/app-1.js"} {
		rec := get(t, h, "GET", p)
		want := map[string]string{
			"Content-Security-Policy": webCSP,
			"X-Content-Type-Options":  "nosniff",
			"Referrer-Policy":         "no-referrer",
		}
		for k, v := range want {
			if got := rec.Header().Get(k); got != v {
				t.Errorf("%s %s = %q, want %q", p, k, got, v)
			}
		}
		if rec.Header().Get("Strict-Transport-Security") != "" {
			t.Errorf("%s: HSTS sin ARGOS_PUBLIC_URL https", p)
		}
	}
	if !strings.Contains(webCSP, "script-src 'self';") || strings.Contains(webCSP, "script-src 'self' 'unsafe") {
		t.Error("los scripts deben quedar estrictos")
	}
	// La CSP de /api/ no se debilita.
	rec := get(t, h, "GET", "/api/v1/auth/me")
	if got := rec.Header().Get("Content-Security-Policy"); got != "default-src 'self'; frame-ancestors 'none'" {
		t.Errorf("CSP de /api = %q", got)
	}
}

func TestWebReservedPaths(t *testing.T) {
	t.Parallel()
	_, web := newWebDir(t)
	h := newWebAPI(t, web)
	for _, p := range []string{"/api/v1/nada", "/api/", "/dl/x/y/z", "/install.exe", "/api/v1/hosts/x/y/z"} {
		rec := get(t, h, "GET", p)
		if rec.Code != 404 {
			t.Errorf("%s: status = %d, want 404", p, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("%s: Content-Type = %q, want JSON", p, ct)
		}
		if strings.Contains(rec.Body.String(), "<title>") {
			t.Errorf("%s: devolvio index.html", p)
		}
	}
	if rec := get(t, h, "GET", "/api/v1/auth/me"); rec.Code != 401 {
		t.Errorf("/api/v1/auth/me = %d, want 401", rec.Code)
	}
	if rec := get(t, h, "GET", "/health"); rec.Code != 200 {
		t.Errorf("/health = %d, want 200", rec.Code)
	}
}

func TestWebMethodsAndHost(t *testing.T) {
	t.Parallel()
	_, web := newWebDir(t)
	h := newWebAPI(t, web)
	for _, m := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		if rec := get(t, h, m, "/"); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s / = %d, want 405", m, rec.Code)
		}
	}
	rec := get(t, h, "GET", "/", func(r *http.Request) { r.Host = "evil.example" })
	if rec.Code != http.StatusMisdirectedRequest {
		t.Errorf("Host ajeno = %d, want 421", rec.Code)
	}
	// Mismo origen desde el navegador: login por POST con Origin == Host no lo frena CrossOriginProtection.
	rec = get(t, h, "POST", "/api/v1/auth/login", func(r *http.Request) {
		r.Header.Set("Origin", "http://localhost:8080")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
	})
	if rec.Code == http.StatusForbidden {
		t.Errorf("POST same-origin bloqueado por CSRF: %s", rec.Body.String())
	}
}

func TestWebSymlinkOutside(t *testing.T) {
	t.Parallel()
	parent, web := newWebDir(t)
	if err := os.Symlink(filepath.Join(parent, "secret.txt"), filepath.Join(web, "assets", "leak.txt")); err != nil {
		t.Skipf("sin permiso pa crear symlinks: %v", err)
	}
	h := newWebAPI(t, web)
	rec := get(t, h, "GET", "/assets/leak.txt")
	if rec.Code != 404 || strings.Contains(rec.Body.String(), "TOP-SECRET") {
		t.Errorf("symlink fuera del directorio: status %d, body %q", rec.Code, rec.Body.String())
	}
}

func TestWebDisabled(t *testing.T) {
	t.Parallel()
	h, _ := newDistAPI(t, Options{})
	for _, p := range []string{"/", "/hosts/x"} {
		if rec := get(t, h, "GET", p); rec.Code != 404 {
			t.Errorf("%s sin ARGOS_WEB_DIR = %d, want 404", p, rec.Code)
		}
	}
}

func TestOpenWebDirInvalid(t *testing.T) {
	t.Parallel()
	empty := t.TempDir()
	tests := []struct{ name, dir string }{
		{"no existe", filepath.Join(empty, "nada")},
		{"sin index.html", empty},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root, err := OpenWebDir(tt.dir)
			if err == nil {
				_ = root.Close()
				t.Fatal("esperaba error")
			}
		})
	}
}
