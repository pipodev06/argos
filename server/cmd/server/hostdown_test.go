package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"argos/server/internal/store"
)

type fakeMarker struct {
	mu        sync.Mutex
	calls     []time.Time
	gotAfter  time.Duration
	result    []store.StaleHost
	err       error
	blockOnce bool // la primera llamada espera a que se cancele el contexto
}

func (f *fakeMarker) MarkStaleHosts(ctx context.Context, downAfter time.Duration) ([]store.StaleHost, error) {
	f.mu.Lock()
	f.calls = append(f.calls, time.Now())
	f.gotAfter = downAfter
	block := f.blockOnce
	f.mu.Unlock()
	if block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return f.result, f.err
}

func (f *fakeMarker) callTimes() []time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Time(nil), f.calls...)
}

// syncBuffer es un bytes.Buffer seguro pa que lo escriba el detector y lo lea el test.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestMarkStaleHostsLogsEachHost(t *testing.T) {
	t.Parallel()
	logs := &syncBuffer{}
	f := &fakeMarker{result: []store.StaleHost{
		{ID: "id-1", Hostname: "srv-01", Status: "down"},
		{ID: "id-2", Hostname: "laptop-02", Status: "offline"},
	}}
	markStaleHosts(context.Background(), slog.New(slog.NewJSONHandler(logs, nil)), f, 2*time.Minute)

	got := logs.String()
	for _, want := range []string{`"host caido"`, `"host_id":"id-1"`, `"hostname":"srv-01"`, `"host offline"`, `"host_id":"id-2"`, `"hostname":"laptop-02"`} {
		if !strings.Contains(got, want) {
			t.Errorf("log no contiene %s: %s", want, got)
		}
	}
	// Solo el down es un WARN: el offline (laptop que duerme) no requiere atencion.
	for _, line := range strings.Split(strings.TrimSpace(got), "\n") {
		if strings.Contains(line, `"laptop-02"`) && strings.Contains(line, `"level":"WARN"`) {
			t.Errorf("un host offline no debe loguearse como WARN: %s", line)
		}
		if strings.Contains(line, `"srv-01"`) && !strings.Contains(line, `"level":"WARN"`) {
			t.Errorf("un host down debe loguearse como WARN: %s", line)
		}
	}
	if f.gotAfter != 2*time.Minute {
		t.Errorf("downAfter got %s, want 2m", f.gotAfter)
	}
}

func TestMarkStaleHostsErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		err       error
		wantError bool
	}{
		{name: "error de db se loguea", err: errors.New("db caida"), wantError: true},
		{name: "apagado en curso no es un fallo", err: context.Canceled, wantError: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			logs := &syncBuffer{}
			markStaleHosts(context.Background(), slog.New(slog.NewJSONHandler(logs, nil)), &fakeMarker{err: tt.err}, time.Minute)
			if got := strings.Contains(logs.String(), `"level":"ERROR"`); got != tt.wantError {
				t.Errorf("log de error got %v, want %v (%s)", got, tt.wantError, logs.String())
			}
		})
	}
}

func TestDetectorWaitsGracePeriodThenChecksAndStopsOnCancel(t *testing.T) {
	t.Parallel()
	const downAfter = 150 * time.Millisecond
	f := &fakeMarker{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	start := time.Now()
	done := make(chan struct{})
	go func() {
		defer close(done)
		runHostDownDetector(ctx, slog.New(slog.NewJSONHandler(&syncBuffer{}, nil)), f, downAfter, 10*time.Millisecond)
	}()

	deadline := time.After(5 * time.Second)
	for len(f.callTimes()) < 2 {
		select {
		case <-deadline:
			t.Fatal("el detector no corrio dos veces en 5 s")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if first := f.callTimes()[0]; first.Sub(start) < downAfter {
		t.Errorf("primer chequeo a los %s, want >= %s (gracia tras el arranque)", first.Sub(start), downAfter)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("el detector no termino tras cancelar el contexto")
	}
	n := len(f.callTimes())
	time.Sleep(50 * time.Millisecond)
	if got := len(f.callTimes()); got != n {
		t.Errorf("el detector siguio chequeando tras terminar (%d -> %d llamadas)", n, got)
	}
}

// Cancelar mientras una consulta esta en vuelo tambien termina el detector.
func TestDetectorStopsWhileQueryInFlight(t *testing.T) {
	t.Parallel()
	f := &fakeMarker{blockOnce: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		runHostDownDetector(ctx, slog.New(slog.NewJSONHandler(&syncBuffer{}, nil)), f, time.Millisecond, time.Millisecond)
	}()
	deadline := time.After(5 * time.Second)
	for len(f.callTimes()) == 0 {
		select {
		case <-deadline:
			t.Fatal("el detector no llego a consultar")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("el detector no termino con una consulta en vuelo")
	}
}
