package updater

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSwapper registra el binario descargado en vez de reemplazar nada.
type fakeSwapper struct {
	dir      string
	swapped  []byte
	swapCall int
	swapErr  error
}

func (f *fakeSwapper) Stage() (*os.File, error) { return os.CreateTemp(f.dir, ".stage-*") }

func (f *fakeSwapper) Swap(_ context.Context, staged string) error {
	f.swapCall++
	if f.swapErr != nil {
		return f.swapErr
	}
	data, err := os.ReadFile(staged)
	f.swapped = data
	return err
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// newDist sirve un manifiesto con version ver y un binario body; hashOverride/sizeOverride permiten falsearlos.
func newDist(t *testing.T, ver string, body []byte, hashOverride string, sizeOverride int64) *httptest.Server {
	t.Helper()
	hash, size := sum(body), int64(len(body))
	if hashOverride != "" {
		hash = hashOverride
	}
	if sizeOverride != 0 {
		size = sizeOverride
	}
	name := "argos-agent_linux_amd64"
	mux := http.NewServeMux()
	mux.HandleFunc("GET /dl/latest/manifest.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"version":%q,"files":[{"name":%q,"os":"linux","arch":"amd64","sha256":%q,"size":%d}]}`, ver, name, hash, size)
	})
	mux.HandleFunc("GET /dl/"+ver+"/"+name, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func run(t *testing.T, srv *httptest.Server, version string, sw *fakeSwapper) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := Run(context.Background(), Options{
		Server: srv.URL, Version: version, HTTP: srv.Client(), GOOS: "linux", GOARCH: "amd64", Swapper: sw, Out: &out,
	})
	return out.String(), err
}

func TestRun(t *testing.T) {
	t.Parallel()
	binary := []byte("binario-nuevo")
	tests := []struct {
		name      string
		serverVer string
		hash      string
		size      int64
		current   string
		wantErr   string
		wantSwap  bool
		wantOut   string
	}{
		{name: "actualiza", serverVer: "1.1.0", current: "1.0.0", wantSwap: true, wantOut: "actualizado a 1.1.0"},
		{name: "misma version", serverVer: "1.0.0", current: "1.0.0", wantOut: "ultima version"},
		{name: "build dev se niega", serverVer: "1.1.0", current: "dev", wantErr: "desarrollo"},
		{name: "hash incorrecto", serverVer: "1.1.0", current: "1.0.0", hash: strings.Repeat("0", 64), wantErr: "SHA-256"},
		{name: "tamano distinto", serverVer: "1.1.0", current: "1.0.0", size: int64(len(binary)) + 5, wantErr: "tamano"},
		{name: "downgrade se niega", serverVer: "0.9.0", current: "1.0.0", wantErr: "downgrade"},
		{name: "1.10 es mas nueva que 1.9", serverVer: "1.10.0", current: "1.9.0", wantSwap: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := newDist(t, tt.serverVer, binary, tt.hash, tt.size)
			sw := &fakeSwapper{dir: t.TempDir()}
			out, err := run(t, srv, tt.current, sw)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err got %v, want que contenga %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if (sw.swapCall == 1) != tt.wantSwap {
				t.Errorf("Swap llamado %d veces, wantSwap %v", sw.swapCall, tt.wantSwap)
			}
			if tt.wantSwap && !bytes.Equal(sw.swapped, binary) {
				t.Errorf("binario entregado a Swap got %q, want %q", sw.swapped, binary)
			}
			if !strings.Contains(out, tt.wantOut) {
				t.Errorf("salida got %q, want que contenga %q", out, tt.wantOut)
			}
			if left, _ := filepath.Glob(filepath.Join(sw.dir, ".stage-*")); len(left) != 0 {
				t.Errorf("quedaron archivos temporales: %v", left)
			}
		})
	}
}

func TestRunServerErrors(t *testing.T) {
	t.Parallel()
	for name, h := range map[string]http.HandlerFunc{
		"404":             func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) },
		"manifiesto roto": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("{no json")) },
		"version rara":    func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"version":"../x","files":[]}`)) },
		"sin mi os/arch":  func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"version":"2.0.0","files":[]}`)) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(h)
			defer srv.Close()
			sw := &fakeSwapper{dir: t.TempDir()}
			if _, err := run(t, srv, "1.0.0", sw); err == nil || sw.swapCall != 0 {
				t.Errorf("err got %v swaps %d, want error y ningun Swap", err, sw.swapCall)
			}
		})
	}
}

// fakeSvc simula el servicio del SO sobre un directorio temporal.
type fakeSvc struct {
	mu          sync.Mutex
	exe         string
	running     bool
	startFail   bool
	startedWith []string
	calls       []string
}

func (f *fakeSvc) ExePath() (string, error) { return f.exe, nil }

func (f *fakeSvc) Status(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.running {
		return "instalado; estado: active", nil
	}
	return "instalado; estado: inactive", nil
}

func (f *fakeSvc) Stop(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "stop")
	f.running = false
	return nil
}

func (f *fakeSvc) Start(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "start")
	data, err := os.ReadFile(f.exe)
	if err != nil {
		return err
	}
	f.startedWith = append(f.startedWith, string(data))
	// El binario nuevo "no arranca": el servicio queda caido aunque Start no falle.
	f.running = !f.startFail || string(data) != "NUEVO"
	return nil
}

func newSwapper(t *testing.T, running, startFail bool) (FileSwapper, *fakeSvc) {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "argos-agent")
	if err := os.WriteFile(exe, []byte("VIEJO"), 0o755); err != nil {
		t.Fatal(err)
	}
	svc := &fakeSvc{exe: exe, running: running, startFail: startFail}
	return FileSwapper{
		Svc: svc, IsRunning: func(l string) bool { return strings.Contains(l, "estado: active") },
		Logger: slog.New(slog.DiscardHandler), Settle: time.Millisecond,
	}, svc
}

func stage(t *testing.T, s FileSwapper, content string) string {
	t.Helper()
	f, err := s.Stage()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return f.Name()
}

func TestFileSwapperReplacesAndRestarts(t *testing.T) {
	t.Parallel()
	s, svc := newSwapper(t, true, false)
	if err := s.Swap(context.Background(), stage(t, s, "NUEVO")); err != nil {
		t.Fatalf("Swap: %v", err)
	}
	got, err := os.ReadFile(svc.exe)
	if err != nil || string(got) != "NUEVO" {
		t.Errorf("binario instalado got %q (%v), want NUEVO", got, err)
	}
	if !svc.running || strings.Join(svc.calls, ",") != "stop,start" {
		t.Errorf("running %v calls %v, want reinicio (stop,start)", svc.running, svc.calls)
	}
	if _, err := os.Stat(svc.exe + ".old"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("el respaldo .old deberia borrarse tras un exito: %v", err)
	}
}

func TestFileSwapperRestoresOldWhenNewFailsToStart(t *testing.T) {
	t.Parallel()
	s, svc := newSwapper(t, true, true)
	err := s.Swap(context.Background(), stage(t, s, "NUEVO"))
	if err == nil || !strings.Contains(err.Error(), "restauro") {
		t.Fatalf("err got %v, want error que diga que se restauro la anterior", err)
	}
	got, rerr := os.ReadFile(svc.exe)
	if rerr != nil || string(got) != "VIEJO" {
		t.Errorf("binario instalado got %q (%v), want VIEJO restaurado", got, rerr)
	}
	if !svc.running {
		t.Error("el servicio debe quedar en ejecucion con la version anterior")
	}
	if want := []string{"NUEVO", "VIEJO"}; strings.Join(svc.startedWith, ",") != strings.Join(want, ",") {
		t.Errorf("arranques con %v, want %v", svc.startedWith, want)
	}
}

func TestFileSwapperKeepsStoppedServiceStopped(t *testing.T) {
	t.Parallel()
	s, svc := newSwapper(t, false, false)
	if err := s.Swap(context.Background(), stage(t, s, "NUEVO")); err != nil {
		t.Fatalf("Swap: %v", err)
	}
	if got, _ := os.ReadFile(svc.exe); string(got) != "NUEVO" || len(svc.calls) != 0 {
		t.Errorf("binario %q calls %v, want NUEVO y sin tocar el servicio", got, svc.calls)
	}
}

func TestFileSwapperStageRequiresInstalledBinary(t *testing.T) {
	t.Parallel()
	s, svc := newSwapper(t, true, false)
	if err := os.Remove(svc.exe); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Stage(); err == nil {
		t.Error("sin binario instalado Stage debe fallar")
	}
}

func TestOlder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		a, b string
		want bool
	}{
		{"0.9.0", "1.0.0", true}, {"1.0.0", "1.0.0", false}, {"1.10.0", "1.9.0", false},
		{"1.0", "1.0.1", true}, {"abc", "1.0.0", false}, {"1.0.0-rc1", "1.0.0", false},
	}
	for _, tt := range tests {
		if got := older(tt.a, tt.b); got != tt.want {
			t.Errorf("older(%q, %q) got %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}
