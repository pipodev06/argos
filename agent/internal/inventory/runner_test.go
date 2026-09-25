package inventory

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	psnet "github.com/shirou/gopsutil/v4/net"
)

// fakeServer registra los envios y responde con el status que se le programe.
type fakeServer struct {
	mu     sync.Mutex
	status func(body map[string]any) int
	bodies []map[string]any
	auths  []string
	srv    *httptest.Server
}

func newFakeServer(t *testing.T, status func(body map[string]any) int) *fakeServer {
	t.Helper()
	f := &fakeServer{status: status}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || r.Method != http.MethodPost || r.URL.Path != inventoryPath {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.bodies = append(f.bodies, body)
		f.auths = append(f.auths, r.Header.Get("Authorization"))
		st := f.status
		f.mu.Unlock()
		w.WriteHeader(st(body))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeServer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.bodies)
}

func (f *fakeServer) setStatus(st func(body map[string]any) int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = st
}

func newTestWorker(t *testing.T, f *fakeServer, conns *[]psnet.ConnectionStat, now *time.Time, logs *bytes.Buffer) *worker {
	t.Helper()
	src := fakeSources(nil, nil)
	src.Conns = func(context.Context) ([]psnet.ConnectionStat, error) { return *conns, nil }
	m, ok := buildModule(ModulePorts, src, UpdatesConfig{})
	if !ok {
		t.Fatal("modulo ports no disponible")
	}
	lg := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return &worker{mod: m, cl: &client{url: f.srv.URL + inventoryPath, token: "tok-secreto", http: f.srv.Client()}, log: lg, now: func() time.Time { return *now }}
}

func noContent(map[string]any) int { return http.StatusNoContent }

func TestWorkerSendsOnlyOnChange(t *testing.T) {
	t.Parallel()
	f := newFakeServer(t, noContent)
	conns := []psnet.ConnectionStat{listen("0.0.0.0", 80, 1)}
	now := time.Unix(1_800_000_000, 0)
	var logs bytes.Buffer
	w := newTestWorker(t, f, &conns, &now, &logs)

	w.step(t.Context())
	if f.count() != 1 || f.bodies[0]["module"] != "ports" || f.bodies[0]["items"] == nil || f.bodies[0]["collected_at"] == nil {
		t.Fatalf("primer envio incorrecto: %v", f.bodies)
	}
	if f.auths[0] != "Bearer tok-secreto" {
		t.Errorf("auth = %q", f.auths[0])
	}
	now = now.Add(time.Minute)
	w.step(t.Context()) // sin cambios ni latido vencido: no envia
	if f.count() != 1 {
		t.Fatalf("envio sin cambios: %d", f.count())
	}
	conns = append(conns, listen("0.0.0.0", 81, 1))
	now = now.Add(time.Minute)
	w.step(t.Context())
	if f.count() != 2 {
		t.Fatalf("no envio el cambio: %d", f.count())
	}
	now = now.Add(6*time.Hour + time.Second) // latido: sin items
	w.step(t.Context())
	if f.count() != 3 {
		t.Fatalf("no envio el latido: %d", f.count())
	}
	hb := f.bodies[2]
	if _, has := hb["items"]; has || hb["rev"] != f.bodies[1]["rev"] {
		t.Errorf("latido incorrecto: %v", hb)
	}
}

func TestWorkerMinGapBetweenChanges(t *testing.T) {
	t.Parallel()
	f := newFakeServer(t, noContent)
	conns := []psnet.ConnectionStat{listen("0.0.0.0", 80, 1)}
	now := time.Unix(1_800_000_000, 0)
	var logs bytes.Buffer
	w := newTestWorker(t, f, &conns, &now, &logs)
	w.step(t.Context())
	conns = append(conns, listen("0.0.0.0", 81, 1))
	now = now.Add(10 * time.Second)
	if d := w.step(t.Context()); f.count() != 1 || d != 20*time.Second {
		t.Errorf("min gap: envios=%d espera=%s, want 1 y 20s", f.count(), d)
	}
}

func TestWorkerNeedFullResendsComplete(t *testing.T) {
	t.Parallel()
	f := newFakeServer(t, func(body map[string]any) int {
		if _, full := body["items"]; !full {
			return http.StatusConflict
		}
		return http.StatusNoContent
	})
	conns := []psnet.ConnectionStat{listen("0.0.0.0", 80, 1)}
	now := time.Unix(1_800_000_000, 0)
	var logs bytes.Buffer
	w := newTestWorker(t, f, &conns, &now, &logs)
	w.step(t.Context())
	now = now.Add(7 * time.Hour)
	w.step(t.Context()) // latido -> 409
	w.step(t.Context()) // reenvio completo
	if f.count() != 3 {
		t.Fatalf("envios = %d, want 3 (completo, latido, completo)", f.count())
	}
	if _, has := f.bodies[2]["items"]; !has {
		t.Errorf("el reenvio tras need_full no fue completo: %v", f.bodies[2])
	}
}

func TestWorkerNotFoundDisablesQuietly(t *testing.T) {
	t.Parallel()
	for _, code := range []int{http.StatusNotFound, http.StatusMethodNotAllowed} {
		f := newFakeServer(t, func(map[string]any) int { return code })
		conns := []psnet.ConnectionStat{listen("0.0.0.0", 80, 1)}
		now := time.Unix(1_800_000_000, 0)
		var logs bytes.Buffer
		w := newTestWorker(t, f, &conns, &now, &logs)
		for range 5 {
			d := w.step(t.Context())
			if d < 30*time.Minute || d > unsupportedMax {
				t.Fatalf("%d: espera %s fuera de [30m, 24h]", code, d)
			}
		}
		if n := strings.Count(logs.String(), "level=WARN"); n != 1 {
			t.Errorf("%d: %d avisos, want 1 (sin spam):\n%s", code, n, logs.String())
		}
		if w.sent {
			t.Errorf("%d: no debe marcarse como enviado", code)
		}
		// Sin perdida: cuando el servidor lo soporte, el envio siguiente es completo.
		f.setStatus(noContent)
		w.step(t.Context())
		if !w.sent || w.unsupported != 0 {
			t.Errorf("%d: no se reactivo", code)
		}
		if _, has := f.bodies[len(f.bodies)-1]["items"]; !has {
			t.Errorf("%d: el primer envio tras reactivar no fue completo", code)
		}
	}
}

func TestWorkerTransientFailureWarnsOnceAndBacksOff(t *testing.T) {
	t.Parallel()
	f := newFakeServer(t, func(map[string]any) int { return http.StatusInternalServerError })
	conns := []psnet.ConnectionStat{listen("0.0.0.0", 80, 1)}
	now := time.Unix(1_800_000_000, 0)
	var logs bytes.Buffer
	w := newTestWorker(t, f, &conns, &now, &logs)
	for range 4 {
		if d := w.step(t.Context()); d > failMax {
			t.Fatalf("espera %s pasa el tope", d)
		}
	}
	if n := strings.Count(logs.String(), "level=WARN"); n != 1 {
		t.Errorf("%d avisos, want 1", n)
	}
	if strings.Contains(logs.String(), "tok-secreto") {
		t.Errorf("el token aparece en los logs")
	}
}

func TestRunWithNoModulesDoesNothing(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	calls := 0
	hit := func() { mu.Lock(); calls++; mu.Unlock() }
	src := Sources{
		Conns:    func(context.Context) ([]psnet.ConnectionStat, error) { hit(); return nil, nil },
		Procs:    func(context.Context) ([]ProcInfo, error) { hit(); return nil, nil },
		Services: func(context.Context) ([]Service, error) { hit(); return nil, nil },
	}
	f := newFakeServer(t, noContent)
	done := make(chan struct{})
	go func() {
		Run(t.Context(), Options{Endpoint: f.srv.URL, Token: "t", Modules: nil, Sources: &src})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run con modulos vacios no volvio")
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 0 || f.count() != 0 {
		t.Errorf("modulos apagados ejecutaron algo: colectores=%d envios=%d", calls, f.count())
	}
}

func TestRunOnlyRunsEnabledModulesAndStops(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	calls := map[string]int{}
	count := func(k string) { mu.Lock(); calls[k]++; mu.Unlock() }
	src := Sources{
		Conns: func(context.Context) ([]psnet.ConnectionStat, error) { count("ports"); return nil, nil },
		Procs: func(context.Context) ([]ProcInfo, error) { count("processes"); return nil, nil },
	}
	f := newFakeServer(t, noContent)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() {
		// services no esta disponible en estas fuentes (Services nil): se omite con aviso.
		Run(ctx, Options{Endpoint: f.srv.URL, Token: "t", HTTPClient: f.srv.Client(), Modules: []string{ModulePorts, ModuleServices}, Sources: &src})
		close(done)
	}()
	deadline := time.After(startJitter + 5*time.Second)
	for f.count() < 1 {
		select {
		case <-deadline:
			t.Fatal("no hubo envio")
		case <-time.After(20 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run no termino al cancelar")
	}
	mu.Lock()
	defer mu.Unlock()
	if calls["processes"] != 0 || calls["ports"] == 0 {
		t.Errorf("colectores ejecutados: %v", calls)
	}
}
