package agent

import (
	"context"
	"testing"
	"time"

	"argos/agent/internal/checks"
)

type fakeChecks struct {
	rev     string
	results []checks.Result
}

func (f *fakeChecks) Rev() string { return f.rev }
func (f *fakeChecks) Drain() []checks.Result {
	out := f.results
	f.results = nil
	return out
}

func TestRunAttachesCheckRevAndResults(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sub := &fakeSubmitter{sent: make(chan struct{}, 10)}
	fc := &fakeChecks{rev: "0123456789abcdef", results: []checks.Result{{CheckID: "u1", Status: "up", Detail: "ok"}}}
	o := opts(&fakeCollector{}, time.Hour)
	o.Checks = fc
	done := make(chan error, 1)
	go func() { done <- Run(ctx, sub, o) }()
	<-sub.sent
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
	sub.mu.Lock()
	defer sub.mu.Unlock()
	r := sub.reports[0]
	if r.ChecksRev != "0123456789abcdef" || len(r.Services) != 1 || r.Services[0].CheckID != "u1" {
		t.Errorf("reporte sin chequeos: rev=%q services=%+v", r.ChecksRev, r.Services)
	}
}

func TestRunWithoutChecksOmitsThem(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sub := &fakeSubmitter{sent: make(chan struct{}, 10)}
	done := make(chan error, 1)
	go func() { done <- Run(ctx, sub, opts(&fakeCollector{}, time.Hour)) }()
	<-sub.sent
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
	sub.mu.Lock()
	defer sub.mu.Unlock()
	if r := sub.reports[0]; r.ChecksRev != "" || r.Services != nil {
		t.Errorf("reporte con chequeos sin runner: %+v", r)
	}
}
