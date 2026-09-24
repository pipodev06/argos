package checks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// waitResults junta resultados hasta tener n (o falla por timeout).
func waitResults(t *testing.T, r *Runner, n int, d time.Duration) []Result {
	t.Helper()
	var all []Result
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		all = append(all, r.Drain()...)
		if len(all) >= n {
			return all
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("llegaron %d resultados, want %d", len(all), n)
	return nil
}

func serviceChecks(n int) []Check {
	out := make([]Check, n)
	for i := range out {
		out[i] = Check{ID: fmt.Sprintf("id-%02d", i), Type: TypeService, Name: fmt.Sprintf("svc %d", i), ServiceName: "nginx", IntervalS: 3600, TimeoutMS: 2000}
	}
	return out
}

func TestApplyCapsAtTwentyAndReportsUnknown(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	prober := newFakeProber(func(int) (bool, error) { return true, nil }, 0)
	r := newTestRunner(t, Options{Service: prober, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	r.Apply(t.Context(), Set{Rev: "r1", Items: serviceChecks(25)})

	res := waitResults(t, r, 25, 5*time.Second)
	byID := map[string]Result{}
	for _, x := range res {
		byID[x.CheckID] = x
	}
	for i := range 25 {
		x := byID[fmt.Sprintf("id-%02d", i)]
		wantStatus, wantDetail := StatusUp, DetailOK
		if i >= MaxChecks {
			wantStatus, wantDetail = StatusUnknown, DetailUnsupported
		}
		if x.Status != wantStatus || x.Detail != wantDetail {
			t.Errorf("chequeo %d: got %s/%s, want %s/%s", i, x.Status, x.Detail, wantStatus, wantDetail)
		}
	}
	if calls, _ := prober.stats(); calls != MaxChecks {
		t.Errorf("se ejecutaron %d chequeos, want %d", calls, MaxChecks)
	}
	if !strings.Contains(logs.String(), `"level":"WARN"`) {
		t.Error("falta el WARN por exceder el limite")
	}
}

func TestConcurrencyIsBounded(t *testing.T) {
	t.Parallel()
	prober := newFakeProber(func(int) (bool, error) { return true, nil }, 60*time.Millisecond)
	r := newTestRunner(t, Options{Service: prober})
	r.Apply(t.Context(), Set{Rev: "r1", Items: serviceChecks(12)})
	waitResults(t, r, 12, 5*time.Second)
	if _, maxSeen := prober.stats(); maxSeen > MaxConcurrent || maxSeen < 2 {
		t.Errorf("concurrencia maxima = %d, want entre 2 y %d", maxSeen, MaxConcurrent)
	}
}

func TestEffectiveInterval(t *testing.T) {
	t.Parallel()
	tests := []struct {
		s    int
		want time.Duration
	}{{0, 30 * time.Second}, {5, 30 * time.Second}, {30, 30 * time.Second}, {90, 90 * time.Second}}
	for _, tt := range tests {
		if got := effectiveInterval(Check{IntervalS: tt.s}, MinInterval*time.Second); got != tt.want {
			t.Errorf("intervalo %ds: got %v, want %v", tt.s, got, tt.want)
		}
	}
}

func TestRunsEachCheckOnItsInterval(t *testing.T) {
	t.Parallel()
	prober := newFakeProber(func(int) (bool, error) { return true, nil }, 0)
	r := newTestRunner(t, Options{Service: prober, MinInterval: time.Second})
	c := serviceChecks(1)[0]
	c.IntervalS = 1
	start := time.Now()
	r.Apply(t.Context(), Set{Rev: "r1", Items: []Check{c}})
	res := waitResults(t, r, 3, 6*time.Second) // primera ejecucion inmediata + 2 mas
	if el := time.Since(start); el < 1800*time.Millisecond {
		t.Errorf("3 resultados en %v: el intervalo de 1s no se respeta", el)
	}
	for _, x := range res {
		if x.CheckID != c.ID || x.Name != c.Name || x.TS == 0 {
			t.Errorf("resultado mal formado: %+v", x)
		}
	}
}

func TestRetries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		retries   int
		fn        func(call int) (bool, error)
		wantCalls int
		want      string
	}{
		{"sin reintentos", 0, func(int) (bool, error) { return false, nil }, 1, StatusDown},
		{"se recupera al 3er intento", 2, func(call int) (bool, error) { return call >= 3, nil }, 3, StatusUp},
		{"agota los reintentos", 2, func(int) (bool, error) { return false, nil }, 3, StatusDown},
		{"tope de 3 reintentos", 10, func(int) (bool, error) { return false, nil }, 4, StatusDown},
		{"unknown no se reintenta", 3, func(int) (bool, error) { return false, ErrServiceDenied }, 1, StatusUnknown},
		{"up no se reintenta", 3, func(int) (bool, error) { return true, nil }, 1, StatusUp},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prober := newFakeProber(tt.fn, 0)
			r := newTestRunner(t, Options{Service: prober})
			got := r.attempts(t.Context(), Check{Type: TypeService, ServiceName: "x", Retries: tt.retries, TimeoutMS: 1000})
			if calls, _ := prober.stats(); got.status != tt.want || calls != tt.wantCalls {
				t.Errorf("got %s con %d intentos, want %s con %d", got.status, calls, tt.want, tt.wantCalls)
			}
		})
	}
}

func TestApplyReconciles(t *testing.T) {
	t.Parallel()
	prober := newFakeProber(func(int) (bool, error) { return true, nil }, 0)
	r := newTestRunner(t, Options{Service: prober})
	if r.Rev() != "" {
		t.Fatalf("rev inicial = %q, want vacia", r.Rev())
	}
	items := serviceChecks(3)
	r.Apply(t.Context(), Set{Rev: "r1", Items: items})
	waitResults(t, r, 3, 3*time.Second)
	if r.Rev() != "r1" || r.running() != 3 {
		t.Fatalf("rev=%q jobs=%d, want r1 y 3", r.Rev(), r.running())
	}

	r.Apply(t.Context(), Set{Rev: "r1", Items: items}) // misma revision: no hace nada
	time.Sleep(50 * time.Millisecond)
	if got := r.Drain(); len(got) != 0 {
		t.Errorf("la misma revision reejecuto %d chequeos", len(got))
	}

	// Se quita uno, se modifica otro (reinicia y corre de inmediato), el tercero sigue.
	items[1].TimeoutMS = 1500
	r.Apply(t.Context(), Set{Rev: "r2", Items: items[1:]})
	got := waitResults(t, r, 1, 3*time.Second)
	if got[0].CheckID != items[1].ID || r.running() != 2 || r.Rev() != "r2" {
		t.Errorf("got %+v jobs=%d rev=%q", got, r.running(), r.Rev())
	}

	r.Apply(t.Context(), Set{Rev: "r3"}) // lista vacia: se detiene todo
	if r.running() != 0 {
		t.Errorf("jobs = %d, want 0", r.running())
	}
}

func TestCancelledContextStopsWithoutResults(t *testing.T) {
	t.Parallel()
	prober := newFakeProber(func(int) (bool, error) { return true, nil }, time.Hour)
	r := NewRunner(Options{Service: prober, OwnIPs: noOwnIPs})
	ctx, cancel := context.WithCancel(t.Context())
	r.Apply(ctx, Set{Rev: "r1", Items: serviceChecks(2)})
	time.Sleep(50 * time.Millisecond)
	cancel()
	r.Close() // espera a las goroutines: si alguna queda colgada, el test se cuelga
	if got := r.Drain(); len(got) != 0 {
		t.Errorf("resultados tras cancelar: %+v", got)
	}
}

func TestResultQueueIsBounded(t *testing.T) {
	t.Parallel()
	r := newTestRunner(t, Options{})
	for i := range maxQueuedResult + 50 {
		r.push(Result{CheckID: fmt.Sprint(i)})
	}
	got := r.Drain()
	if len(got) != maxQueuedResult || got[0].CheckID != "50" {
		t.Errorf("got %d resultados (primero %q), want %d desde 50", len(got), got[0].CheckID, maxQueuedResult)
	}
	if len(r.Drain()) != 0 {
		t.Error("Drain no vacia la cola")
	}
}

func TestSecretNeverLogged(t *testing.T) {
	t.Parallel()
	const secret = "TOP-SECRET-TOKEN-123"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) }))
	t.Cleanup(srv.Close)
	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r := NewRunner(Options{Logger: log, OwnIPs: noOwnIPs})

	items := make([]Check, 0, 25)
	for i := range 25 { // pasa del limite: se loguea el WARN
		items = append(items, Check{ID: fmt.Sprintf("h%02d", i), Type: TypeHTTP, Name: "h", URL: srv.URL, HeaderName: "Authorization", Secret: secret, IntervalS: 3600, TimeoutMS: 1000})
	}
	r.Apply(t.Context(), Set{Rev: "r1", Items: items})
	res := waitResults(t, r, 25, 5*time.Second)
	r.Close()
	log.Info("check", slog.Any("c", items[0]), "err", errors.New("x"))

	if strings.Contains(logs.String(), secret) {
		t.Errorf("el secreto aparece en los logs: %s", logs.String())
	}
	for _, x := range res {
		if strings.Contains(fmt.Sprintf("%+v", x), secret) {
			t.Fatalf("el secreto aparece en un resultado: %+v", x)
		}
	}
}
