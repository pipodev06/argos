package store

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"
)

func bptr(v bool) *bool { return &v }

func f32(v float64) *float64 { return &v }

func TestIngestNewFields(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedHost(t, "h", "up", ago(time.Second))
	id := hostID(t, "h")
	ts := time.Now().Truncate(time.Second)
	boot := ts.Add(-48 * time.Hour)

	smp := Sample{
		Timestamp: ts, CPUPct: 12.5, MemUsedMB: 4096, MemTotalMB: 16000, NetRxBytes: 1, NetTxBytes: 2,
		SwapUsedMB: iptr(0), SwapTotalMB: iptr(2048),
		Load1: f32(0.5), Load5: f32(0.4), Load15: f32(0.3),
		BatteryPct: iptr(87), BatteryCharging: bptr(true),
		Disks: []DiskSample{{"/", 50000, 200000}, {"/data", 90000, 100000}},
		Info: &HostInfoUpdate{
			BootTime: &boot, CPUModel: "Ryzen", CPUCores: iptr(8), CPUThreads: iptr(16), OSName: "Ubuntu", OSVersion: "24.04",
			KernelVersion: "6.8", AgentVersion: "0.4.1", IPs: []netip.Addr{netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("192.168.1.25")},
		},
	}
	if _, err := s.IngestMetrics(ctx, id, smp); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	// metrics: las 14 columnas, con swap 0 como cero real (no NULL).
	var swapU, swapT, batt *int32
	var load1 *float32
	var charging *bool
	if err := testPool.QueryRow(ctx, `SELECT swap_used_mb, swap_total_mb, load1, battery_pct, battery_charging FROM metrics WHERE host_id = $1`, id).
		Scan(&swapU, &swapT, &load1, &batt, &charging); err != nil {
		t.Fatal(err)
	}
	if swapU == nil || *swapU != 0 || swapT == nil || *swapT != 2048 || load1 == nil || *load1 != 0.5 || batt == nil || *batt != 87 || charging == nil || !*charging {
		t.Errorf("metrics got swap=%v/%v load1=%v batt=%v charging=%v", swapU, swapT, load1, batt, charging)
	}
	// disk_samples: una fila por montaje.
	if n := count(t, `SELECT count(*) FROM disk_samples WHERE host_id = $1`, id); n != 2 {
		t.Errorf("disk_samples got %d, want 2", n)
	}
	// host_latest: disco mas lleno = /data (90 %).
	var pct float64
	var mount string
	var mem int
	if err := testPool.QueryRow(ctx, `SELECT disk_max_pct, disk_max_mount, mem_used_mb FROM host_latest WHERE host_id = $1`, id).Scan(&pct, &mount, &mem); err != nil {
		t.Fatal(err)
	}
	if pct != 90 || mount != "/data" || mem != 4096 {
		t.Errorf("host_latest got %v %q mem=%d, want 90 /data 4096", pct, mount, mem)
	}
	// host_info.
	var model string
	var ips []netip.Addr
	if err := testPool.QueryRow(ctx, `SELECT cpu_model, ips FROM host_info WHERE host_id = $1`, id).Scan(&model, &ips); err != nil {
		t.Fatal(err)
	}
	if model != "Ryzen" || len(ips) != 2 || ips[1].String() != "192.168.1.25" {
		t.Errorf("host_info got %q %v", model, ips)
	}

	// Duplicar el reporte (mismo ts) no duplica discos ni falla.
	if _, err := s.IngestMetrics(ctx, id, smp); err != nil {
		t.Fatalf("reingest: %v", err)
	}
	if n := count(t, `SELECT count(*) FROM disk_samples WHERE host_id = $1`, id); n != 2 {
		t.Errorf("tras reingest disk_samples got %d, want 2", n)
	}
}

// Agente viejo (sin campos nuevos) y reportes posteriores sin discos: los NULL son NULL y el disco previo se conserva.
func TestIngestOldAgentAndDiskCarryOver(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedHost(t, "h", "up", ago(time.Second))
	id := hostID(t, "h")
	t0 := time.Now().Truncate(time.Second).Add(-time.Minute)

	if _, err := s.IngestMetrics(ctx, id, Sample{Timestamp: t0, CPUPct: 1, MemUsedMB: 1, MemTotalMB: 2}); err != nil {
		t.Fatal(err)
	}
	var swap, batt *int32
	var load *float32
	var charging *bool
	if err := testPool.QueryRow(ctx, `SELECT swap_used_mb, load1, battery_pct, battery_charging FROM metrics WHERE host_id = $1`, id).Scan(&swap, &load, &batt, &charging); err != nil {
		t.Fatal(err)
	}
	if swap != nil || load != nil || batt != nil || charging != nil {
		t.Errorf("un agente viejo debe dejar NULL (no cero): %v %v %v %v", swap, load, batt, charging)
	}
	if n := count(t, `SELECT count(*) FROM disk_samples`) + count(t, `SELECT count(*) FROM host_info`); n != 0 {
		t.Errorf("sin discos ni host_info no se escribe nada, got %d filas", n)
	}
	if n := count(t, `SELECT count(*) FROM host_latest WHERE host_id = $1 AND disk_max_pct IS NULL`, id); n != 1 {
		t.Errorf("host_latest debe existir sin disco")
	}

	// Un reporte con disco y luego uno sin: disk_max_* se conserva.
	if _, err := s.IngestMetrics(ctx, id, Sample{Timestamp: t0.Add(10 * time.Second), Disks: []DiskSample{{"C:", 50, 100}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.IngestMetrics(ctx, id, Sample{Timestamp: t0.Add(40 * time.Second), CPUPct: 7}); err != nil {
		t.Fatal(err)
	}
	var pct *float64
	var cpu float64
	if err := testPool.QueryRow(ctx, `SELECT disk_max_pct, cpu_pct FROM host_latest WHERE host_id = $1`, id).Scan(&pct, &cpu); err != nil {
		t.Fatal(err)
	}
	if pct == nil || *pct != 50 || cpu != 7 {
		t.Errorf("host_latest got disk=%v cpu=%v, want 50 y 7", pct, cpu)
	}
}

// El reporte tardio del buffer offline no pisa el ultimo valor (guarda l.ts < EXCLUDED.ts).
func TestIngestLatestIgnoresLateReports(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedHost(t, "h", "up", ago(time.Second))
	id := hostID(t, "h")
	now := time.Now().Truncate(time.Second)

	for _, r := range []struct {
		ts  time.Time
		cpu float64
	}{{now, 50}, {now.Add(-5 * time.Minute), 99}} {
		if _, err := s.IngestMetrics(ctx, id, Sample{Timestamp: r.ts, CPUPct: r.cpu}); err != nil {
			t.Fatal(err)
		}
	}
	var cpu float64
	if err := testPool.QueryRow(ctx, `SELECT cpu_pct FROM host_latest WHERE host_id = $1`, id).Scan(&cpu); err != nil || cpu != 50 {
		t.Errorf("host_latest cpu got %v (%v), want 50 (el tardio no pisa)", cpu, err)
	}
	if n := count(t, `SELECT count(*) FROM metrics WHERE host_id = $1`, id); n != 2 {
		t.Errorf("el reporte tardio si se guarda en metrics: got %d, want 2", n)
	}
}

// host_info solo se reescribe si cambia (boot_time con tolerancia de 60 s).
func TestIngestHostInfoWritesOnlyOnChange(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedHost(t, "h", "up", ago(time.Second))
	id := hostID(t, "h")
	now := time.Now().Truncate(time.Second)
	boot := now.Add(-time.Hour)
	info := func() *HostInfoUpdate {
		b := boot
		return &HostInfoUpdate{BootTime: &b, CPUModel: "X", OSName: "Linux", AgentVersion: "1", CPUCores: iptr(4), IPs: []netip.Addr{netip.MustParseAddr("10.0.0.2")}}
	}
	updatedAt := func() time.Time {
		var u time.Time
		if err := testPool.QueryRow(ctx, `SELECT updated_at FROM host_info WHERE host_id = $1`, id).Scan(&u); err != nil {
			t.Fatal(err)
		}
		return u
	}
	send := func(i int, hi *HostInfoUpdate) {
		t.Helper()
		if _, err := s.IngestMetrics(ctx, id, Sample{Timestamp: now.Add(time.Duration(i) * time.Second), Info: hi}); err != nil {
			t.Fatal(err)
		}
	}

	send(0, info())
	first := updatedAt()
	time.Sleep(20 * time.Millisecond)

	send(1, info())
	if got := updatedAt(); !got.Equal(first) {
		t.Errorf("host_info sin cambios no debe reescribirse")
	}
	jitter := info()
	nb := boot.Add(30 * time.Second)
	jitter.BootTime = &nb
	send(2, jitter)
	if got := updatedAt(); !got.Equal(first) {
		t.Errorf("boot_time dentro de 60 s no debe reescribir")
	}
	far := info()
	nb2 := boot.Add(10 * time.Minute)
	far.BootTime = &nb2
	send(3, far)
	second := updatedAt()
	if !second.After(first) {
		t.Errorf("boot_time distinto en mas de 60 s debe reescribir")
	}
	changed := info()
	changed.IPs = []netip.Addr{netip.MustParseAddr("10.0.0.9")}
	changed.BootTime = &nb2
	time.Sleep(20 * time.Millisecond)
	send(4, changed)
	if got := updatedAt(); !got.After(second) {
		t.Errorf("una IP distinta debe reescribir")
	}
	// Sin host_info en el reporte no se toca nada.
	before := updatedAt()
	send(5, nil)
	if got := updatedAt(); !got.Equal(before) {
		t.Errorf("un reporte sin host_info no debe tocarlo")
	}
}

// Sin particion pa el dia de los discos: 503 (ErrPartitionMissing) solo si el reporte trae discos; sin discos entra.
func TestIngestDiskPartitionMissing(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	t.Cleanup(func() {
		if err := resetSchema(ctx); err != nil {
			t.Errorf("restaurando schema: %v", err)
		}
	})
	seedHost(t, "h", "up", ago(time.Second))
	id := hostID(t, "h")

	day := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, 3)
	mustExec(t, `DROP TABLE disk_samples_p`+day.Format("20060102"))
	ts := day.Add(12 * time.Hour)

	_, err := s.IngestMetrics(ctx, id, Sample{Timestamp: ts, Disks: []DiskSample{{"/", 1, 2}}})
	if !errors.Is(err, ErrPartitionMissing) {
		t.Fatalf("got %v, want ErrPartitionMissing", err)
	}
	if n := count(t, `SELECT count(*) FROM metrics WHERE host_id = $1`, id); n != 0 {
		t.Errorf("la transaccion debe hacer rollback completo, metrics got %d", n)
	}
	if _, err := s.IngestMetrics(ctx, id, Sample{Timestamp: ts}); err != nil {
		t.Errorf("sin discos no necesita la particion de disk_samples: %v", err)
	}
}

func TestLatestMetricsAndSeriesIncludeNewColumns(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedHost(t, "h", "up", ago(time.Second))
	id := hostID(t, "h")
	base := time.Now().Truncate(time.Minute).Add(-10 * time.Minute)
	for i := range 4 {
		smp := Sample{Timestamp: base.Add(time.Duration(i) * 30 * time.Second), CPUPct: 10, MemUsedMB: 100, MemTotalMB: 200,
			NetRxBytes: int64(i * 100), NetTxBytes: int64(i * 10),
			SwapUsedMB: iptr(100 * (i + 1)), SwapTotalMB: iptr(1000), Load1: f32(float64(i)), BatteryPct: iptr(80 - i), BatteryCharging: bptr(i%2 == 0)}
		if _, err := s.IngestMetrics(ctx, id, smp); err != nil {
			t.Fatal(err)
		}
	}
	last, err := s.LatestMetrics(ctx, id)
	if err != nil || last == nil {
		t.Fatalf("LatestMetrics: %v", err)
	}
	if last.SwapUsedMB == nil || *last.SwapUsedMB != 400 || last.Load1 == nil || *last.Load1 != 3 || last.BatteryPct == nil || *last.BatteryPct != 77 ||
		last.BatteryCharging == nil || *last.BatteryCharging || last.Load5 != nil {
		t.Errorf("latest got %+v", last)
	}
	pts, err := s.MetricSeries(ctx, id, base.Add(-time.Minute), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var withSwap int
	for _, p := range pts {
		if p.SwapUsedMB != nil && *p.SwapUsedMB == 250 && p.Load1 != nil && *p.Load1 == 1.5 && p.BatteryPct != nil && p.Load5 == nil {
			withSwap++
		}
	}
	if withSwap != 1 {
		t.Errorf("serie got %+v, want un bucket con swap 250 (promedio) y load1 1.5, load5 nulo", pts)
	}

	// HostDisks: ultima muestra por montaje dentro de la ventana.
	mustExec(t, `INSERT INTO disk_samples (ts, host_id, used_mb, total_mb, mount) VALUES
		($2, $1, 10, 100, '/'), ($3, $1, 20, 100, '/'), ($3, $1, 5, 50, '/data'), ($4, $1, 1, 1, '/viejo')`,
		id, base.Add(-time.Hour), base, time.Now().Add(-10*24*time.Hour))
	disks, err := s.HostDisks(ctx, id, time.Now())
	if err != nil || len(disks) != 2 || disks[0].Mount != "/" || disks[0].UsedMB != 20 || disks[1].Mount != "/data" {
		t.Errorf("HostDisks got (%+v, %v), want / (20) y /data, sin /viejo (fuera de ventana)", disks, err)
	}
}

func TestHostByIDIncludesGroupAndInfo(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	g := mkGroup(t, s, "g", false, nil)
	seedHost(t, "h", "up", ago(time.Second))
	id := hostID(t, "h")
	if _, err := s.SetHostGroup(ctx, id, &g.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.IngestMetrics(ctx, id, Sample{Timestamp: time.Now(), MemTotalMB: 16000, Info: &HostInfoUpdate{CPUModel: "X", IPs: []netip.Addr{netip.MustParseAddr("::1")}}}); err != nil {
		t.Fatal(err)
	}
	h, err := s.HostByID(ctx, AllScope(), id)
	if err != nil {
		t.Fatal(err)
	}
	if h.Group == nil || h.Group.Name != "g" || h.Info.CPUModel == nil || *h.Info.CPUModel != "X" ||
		h.Info.MemTotalMB == nil || *h.Info.MemTotalMB != 16000 || len(h.Info.IPs) != 1 || h.Info.OSName != nil {
		t.Errorf("HostByID got %+v", h)
	}
	if _, err := s.HostByID(ctx, AllScope(), missingUUID); !errors.Is(err, ErrNotFound) {
		t.Errorf("host inexistente got %v", err)
	}
}

// Los rangos largos leen metrics_hourly con las columnas nuevas, ponderadas por muestras; sin dato = nil.
func TestMetricSeriesHourlyNewColumns(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedHost(t, "h", "up", ago(time.Second))
	id := hostID(t, "h")
	h0 := time.Unix(time.Now().Unix()/10800*10800, 0).UTC().Add(-6 * time.Hour) // alineada a un bucket de 3 h
	// Dos horas del mismo bucket de 3 h: pesos 10 y 30 (swap 100 y 200 -> 175), bateria solo en la primera.
	mustExec(t, `INSERT INTO metrics_hourly (host_id, hour, samples, cpu_avg, swap_used_avg_mb, swap_total_mb, load1_avg, battery_pct_avg)
	             VALUES ($1, $2, 10, 1, 100, 1000, 1, 80), ($1, $3, 30, 1, 200, 1000, 3, NULL)`, id, h0, h0.Add(time.Hour))
	pts, err := s.MetricSeriesHourly(ctx, id, h0.Add(-time.Hour), 3*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 1 {
		t.Fatalf("puntos got %d, want 1 (las dos horas caen en el mismo bucket de 3 h)", len(pts))
	}
	p := pts[0]
	if p.SwapUsedMB == nil || *p.SwapUsedMB != 175 || p.Load1 == nil || *p.Load1 != 2.5 || p.BatteryPct == nil || *p.BatteryPct != 80 ||
		p.SwapTotalMB == nil || *p.SwapTotalMB != 1000 {
		t.Errorf("punto got %+v, want swap 175 (ponderado), load1 2.5, bateria 80 (solo cuenta la hora con dato), swap total 1000", p)
	}
	if p.Load5 != nil || p.Load15 != nil {
		t.Errorf("load5/load15 sin dato deben ser nil: %+v", p)
	}
}
