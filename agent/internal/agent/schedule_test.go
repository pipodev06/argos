package agent

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"argos/agent/internal/quiet"
	"argos/agent/internal/sender"
)

type fakeDisks struct {
	calls int
	err   error
	out   []sender.DiskUsage
}

func (f *fakeDisks) Disks(context.Context) ([]sender.DiskUsage, error) {
	f.calls++
	return f.out, f.err
}

type fakeHost struct {
	info sender.HostInfo
	err  error
}

func (f *fakeHost) HostInfo(context.Context) (sender.HostInfo, error) { return f.info, f.err }

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func TestScheduleDisksFirstReportThenEveryInterval(t *testing.T) {
	t.Parallel()

	clk := &clock{t: time.Unix(1_000_000, 0)}
	d := &fakeDisks{out: []sender.DiskUsage{{Mount: "/", UsedMB: 1, TotalMB: 2}}}
	o := Options{Disks: d, Version: "v", Now: clk.now}
	s := newSchedule(300 * time.Second)
	track := quiet.New(nil)

	steps := []struct {
		advance  time.Duration
		wantDisk bool
	}{
		{0, true}, // primer reporte
		{30 * time.Second, false},
		{30 * time.Second, false},
		{239 * time.Second, false}, // 299 s desde el envio
		{1 * time.Second, true},    // 300 s
		{30 * time.Second, false},
	}
	for i, st := range steps {
		clk.t = clk.t.Add(st.advance)
		var r sender.Report
		s.attach(context.Background(), &r, o, track)
		if got := r.Disks != nil; got != st.wantDisk {
			t.Errorf("paso %d: disks presente=%v, want %v", i, got, st.wantDisk)
		}
	}
	if d.calls != 2 {
		t.Errorf("lecturas de disco got %d, want 2 (no se lee cuando no toca)", d.calls)
	}
}

func TestScheduleDiskFailureRetriesNextCycleAndDoesNotBlockOthers(t *testing.T) {
	t.Parallel()

	clk := &clock{t: time.Unix(1_000_000, 0)}
	d := &fakeDisks{err: errors.New("sin acceso")}
	h := &fakeHost{info: sender.HostInfo{OSName: "os"}}
	o := Options{Disks: d, HostInfo: h, Version: "v", Now: clk.now}
	s := newSchedule(300 * time.Second)
	track := quiet.New(slog.New(slog.DiscardHandler))

	var r sender.Report
	s.attach(context.Background(), &r, o, track)
	if r.Disks != nil || r.HostInfo == nil {
		t.Fatalf("un fallo de discos no debe impedir host_info: %+v", r)
	}
	d.err, d.out = nil, []sender.DiskUsage{{Mount: "/", UsedMB: 1, TotalMB: 2}}
	clk.t = clk.t.Add(30 * time.Second)
	r = sender.Report{}
	s.attach(context.Background(), &r, o, track)
	if r.Disks == nil {
		t.Error("tras el fallo, los discos deben reintentarse en el ciclo siguiente")
	}
}

func TestScheduleEmptyDiskListIsNotRetriedEveryCycle(t *testing.T) {
	t.Parallel()

	clk := &clock{t: time.Unix(1_000_000, 0)}
	d := &fakeDisks{}
	o := Options{Disks: d, Now: clk.now}
	s := newSchedule(300 * time.Second)
	for range 3 {
		var r sender.Report
		s.attach(context.Background(), &r, o, quiet.New(nil))
		if r.Disks != nil {
			t.Fatal("sin montajes el campo debe ir ausente")
		}
		clk.t = clk.t.Add(30 * time.Second)
	}
	if d.calls != 1 {
		t.Errorf("lecturas got %d, want 1", d.calls)
	}
}

func TestScheduleHostInfo(t *testing.T) {
	t.Parallel()

	clk := &clock{t: time.Unix(1_000_000, 0)}
	h := &fakeHost{info: sender.HostInfo{OSName: "os", IPs: []string{"10.0.0.1"}}}
	o := Options{HostInfo: h, Version: "0.4.1", Now: clk.now}
	s := newSchedule(0)
	track := quiet.New(nil)

	setIP := func(ip string) { h.info.IPs = []string{ip} }
	steps := []struct {
		name     string
		advance  time.Duration
		mutate   func()
		wantSent bool
	}{
		{"al arrancar", 0, func() {}, true},
		{"sin cambios", time.Minute, func() {}, false},
		{"cambio de IP a los 2 min: espera (max 1 cada 5 min)", time.Minute, func() { setIP("10.0.0.2") }, false},
		{"otro cambio a los 4 min: sigue esperando", 2 * time.Minute, func() { setIP("10.0.0.3") }, false},
		{"cumplidos 5 min desde el envio: sale con la IP mas reciente", time.Minute, func() {}, true},
		{"vuelve a la red anterior enseguida: no se reenvia", time.Minute, func() { setIP("10.0.0.1") }, false},
		{"5 min despues del ultimo envio: sale", 4 * time.Minute, func() {}, true},
		{"sin cambios durante horas: nada", 23 * time.Hour, func() {}, false},
		{"cumplidas 24 h: refresco aunque no cambie", time.Hour, func() {}, true},
		{"despues del refresco: nada", time.Minute, func() {}, false},
	}
	for _, st := range steps {
		clk.t = clk.t.Add(st.advance)
		st.mutate()
		var r sender.Report
		s.attach(context.Background(), &r, o, track)
		if got := r.HostInfo != nil; got != st.wantSent {
			t.Fatalf("%s: host_info presente=%v, want %v", st.name, got, st.wantSent)
		}
		if r.HostInfo != nil && r.HostInfo.AgentVersion != "0.4.1" {
			t.Errorf("%s: agent_version got %q, want la del binario", st.name, r.HostInfo.AgentVersion)
		}
	}
}
