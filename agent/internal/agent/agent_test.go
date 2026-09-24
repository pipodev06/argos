package agent

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"argos/agent/internal/sender"
)

type fakeSubmitter struct {
	mu      sync.Mutex
	reports []sender.Report
	sent    chan struct{}
	err     error // si no es nil, Send lo devuelve
}

func (f *fakeSubmitter) Send(_ context.Context, r sender.Report) error {
	f.mu.Lock()
	f.reports = append(f.reports, r)
	f.mu.Unlock()
	f.sent <- struct{}{}
	return f.err
}

func (f *fakeSubmitter) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reports)
}

type fakeCollector struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (f *fakeCollector) Collect(context.Context) (sender.Metrics, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return sender.Metrics{}, f.err
	}
	return sender.Metrics{CPUPct: 42, MemUsedMB: 1, MemTotalMB: 2, Net: sender.NetMetrics{RxBytes: 3, TxBytes: 4}}, nil
}

func (f *fakeCollector) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func opts(c Collector, interval time.Duration) Options {
	return Options{
		Hostname: "h", Version: "v1", Interval: interval,
		Collector: c, Now: time.Now, Logger: slog.New(slog.DiscardHandler),
	}
}

func TestRunSendsImmediatelyThenPeriodicallyAndStops(t *testing.T) {
	t.Parallel()

	sub := &fakeSubmitter{sent: make(chan struct{}, 10)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, sub, opts(&fakeCollector{}, 20*time.Millisecond)) }()

	// El primer envio ocurre antes de que venza el primer tick (Interval).
	select {
	case <-sub.sent:
	case <-time.After(10 * time.Second):
		t.Fatal("no hubo envio inmediato")
	}
	select {
	case <-sub.sent:
	case <-time.After(10 * time.Second):
		t.Fatal("no hubo envio periodico")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run got %v, want nil al cancelar", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run no termino tras cancelar el contexto")
	}

	sub.mu.Lock()
	defer sub.mu.Unlock()
	r := sub.reports[0]
	if r.Hostname != "h" || r.AgentVersion != "v1" || r.Timestamp == 0 {
		t.Errorf("reporte got %+v", r)
	}
	if r.Metrics.CPUPct != 42 || r.Metrics.Net.RxBytes != 3 {
		t.Errorf("las metricas del Collector no llegaron al reporte: %+v", r.Metrics)
	}
}

func TestRunSkipsCycleWhenCollectorFails(t *testing.T) {
	t.Parallel()

	col := &fakeCollector{err: errors.New("sin /proc")}
	sub := &fakeSubmitter{sent: make(chan struct{}, 10)}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	if err := Run(ctx, sub, opts(col, 20*time.Millisecond)); err != nil {
		t.Fatalf("Run got %v, want nil (un fallo de medicion no mata al agente)", err)
	}
	if col.count() < 2 {
		t.Errorf("Collect llamado %d veces, want >= 2 (sigue intentando)", col.count())
	}
	if sub.count() != 0 {
		t.Errorf("se enviaron %d reportes sin medicion valida", sub.count())
	}
}

func TestRunStopsOnRejectedToken(t *testing.T) {
	t.Parallel()

	sub := &fakeSubmitter{sent: make(chan struct{}, 10), err: sender.ErrTokenRejected}
	done := make(chan error, 1)
	go func() { done <- Run(context.Background(), sub, opts(&fakeCollector{}, time.Hour)) }()

	select {
	case err := <-done:
		if !errors.Is(err, sender.ErrTokenRejected) {
			t.Errorf("Run got %v, want ErrTokenRejected", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run no termino tras un 401")
	}
	if sub.count() != 1 {
		t.Errorf("reportes got %d, want 1 (no reintenta)", sub.count())
	}
}

func TestRunKeepsGoingOnTransientSendError(t *testing.T) {
	t.Parallel()

	sub := &fakeSubmitter{sent: make(chan struct{}, 10), err: errors.New("red caida")}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, sub, opts(&fakeCollector{}, 20*time.Millisecond)) }()

	for range 2 { // dos ciclos pese al error
		select {
		case <-sub.sent:
		case <-time.After(10 * time.Second):
			t.Fatal("el agente dejo de reportar tras un error transitorio")
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run got %v, want nil", err)
	}
}
