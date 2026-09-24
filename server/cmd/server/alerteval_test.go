package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"argos/server/internal/store"
)

type fakeEvaluator struct {
	mu     sync.Mutex
	calls  int
	result store.EvalResult
	err    error
}

func (f *fakeEvaluator) EvaluateAlerts(context.Context) (store.EvalResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.result, f.err
}

func (f *fakeEvaluator) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestRunAlertEvaluatorWaitsForGraceThenEvaluates(t *testing.T) {
	t.Parallel()
	f := &fakeEvaluator{result: store.EvalResult{Ran: true}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runAlertEvaluator(ctx, slog.New(slog.NewJSONHandler(io.Discard, nil)), f, 10*time.Millisecond, 200*time.Millisecond)
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)
	if n := f.count(); n != 0 {
		t.Errorf("durante la gracia no debe evaluar, got %d llamadas", n)
	}
	deadline := time.Now().Add(2 * time.Second)
	for f.count() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := f.count(); n < 2 {
		t.Errorf("tras la gracia debe evaluar periodicamente, got %d llamadas", n)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("el evaluador no termino al cancelar el contexto")
	}
}

func TestEvaluateAlertsLogging(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		result    store.EvalResult
		err       error
		wantLog   string
		wantLevel string
	}{
		{name: "sin cambios no loguea", result: store.EvalResult{Ran: true}},
		{name: "cambios se loguean", result: store.EvalResult{Ran: true, Opened: 2, Resolved: 1}, wantLog: "alertas evaluadas", wantLevel: `"level":"INFO"`},
		{name: "error real", err: errors.New("db caida"), wantLog: "fallo la evaluacion de alertas", wantLevel: `"level":"ERROR"`},
		{name: "cancelacion no es error", err: context.Canceled},
		{name: "lock ocupado no loguea en info", result: store.EvalResult{Ran: false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf syncBuffer
			logger := slog.New(slog.NewJSONHandler(&buf, nil))
			evaluateAlerts(context.Background(), logger, &fakeEvaluator{result: tt.result, err: tt.err})
			out := buf.String()
			if tt.wantLog == "" && out != "" {
				t.Errorf("no debia loguear, got %s", out)
			}
			if tt.wantLog != "" && (!strings.Contains(out, tt.wantLog) || !strings.Contains(out, tt.wantLevel)) {
				t.Errorf("log got %s, want %q (%s)", out, tt.wantLog, tt.wantLevel)
			}
		})
	}
}
