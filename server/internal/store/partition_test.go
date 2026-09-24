package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// insertHourly inserta un resumen por hora (solo las columnas que usa la lectura).
func insertHourly(t *testing.T, hostID string, hour time.Time, samples int, cpu, memU, memT, rx, tx *float64) {
	t.Helper()
	mustExec(t,
		`INSERT INTO metrics_hourly (host_id, hour, samples, cpu_avg, mem_used_avg_mb, mem_total_mb, net_rx_bps_avg, net_tx_bps_avg)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		hostID, hour, samples, cpu, memU, memT, rx, tx)
}

func TestMetricSeriesHourlyWeightsBySamples(t *testing.T) {
	s := newTestStore(t)
	h := mkHost(t, "srv-hourly")
	other := mkHost(t, "srv-otro")
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC) // alineado a 3 h y a 12 h desde la epoch

	// Bucket de 3 h [00:00, 03:00): horas 00 (60 muestras, cpu 10) y 01 (180 muestras, cpu 50); la 02 no existe.
	insertHourly(t, h, day, 60, ptr(10), ptr(100), ptr(1000), ptr(100), nil)
	insertHourly(t, h, day.Add(time.Hour), 180, ptr(50), ptr(200), ptr(1000), ptr(500), nil)
	// Bucket [03:00, 06:00): una hora con columnas nulas (sin dato) y otra con dato: el nulo no pesa.
	insertHourly(t, h, day.Add(3*time.Hour), 100, nil, nil, nil, nil, nil)
	insertHourly(t, h, day.Add(4*time.Hour), 20, ptr(80), ptr(300), ptr(2000), ptr(7), ptr(9))
	// Bucket [06:00, 09:00): solo nulos -> punto con todo nil (el bucket existe).
	insertHourly(t, h, day.Add(6*time.Hour), 30, nil, nil, nil, nil, nil)
	// Otro host: no debe mezclarse.
	insertHourly(t, other, day, 60, ptr(99), ptr(1), ptr(1), ptr(1), ptr(1))
	// Antes de "from": no debe aparecer.
	insertHourly(t, h, day.Add(-3*time.Hour), 60, ptr(1), ptr(1), ptr(1), ptr(1), ptr(1))

	pts, err := s.MetricSeriesHourly(context.Background(), h, day, 3*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 3 {
		t.Fatalf("puntos got %d, want 3: %+v", len(pts), pts)
	}
	for i, want := range []time.Time{day, day.Add(3 * time.Hour), day.Add(6 * time.Hour)} {
		if !pts[i].TS.Equal(want) || pts[i].TS.Location() != time.UTC {
			t.Errorf("ts[%d] got %v, want %v en UTC", i, pts[i].TS, want)
		}
	}
	// (10*60 + 50*180) / 240 = 40; mem (100*60 + 200*180) / 240 = 175; rx (100*60 + 500*180) / 240 = 400.
	if !near(pts[0].CPUPct, 40) || !near(pts[0].MemUsedMB, 175) || !near(pts[0].MemTotalMB, 1000) || !near(pts[0].NetRxBps, 400) {
		t.Errorf("bucket 0 got cpu=%s mem=%s memT=%s rx=%s, want 40/175/1000/400",
			fmtRate(pts[0].CPUPct), fmtRate(pts[0].MemUsedMB), fmtRate(pts[0].MemTotalMB), fmtRate(pts[0].NetRxBps))
	}
	if pts[0].NetTxBps != nil {
		t.Errorf("tx del bucket 0 got %s, want nil (sin dato en ninguna hora)", fmtRate(pts[0].NetTxBps))
	}
	// Las 100 muestras de la hora con nulos no cuentan: solo pesa la hora con dato.
	if !near(pts[1].CPUPct, 80) || !near(pts[1].NetRxBps, 7) || !near(pts[1].NetTxBps, 9) {
		t.Errorf("bucket 1 got cpu=%s rx=%s tx=%s, want 80/7/9", fmtRate(pts[1].CPUPct), fmtRate(pts[1].NetRxBps), fmtRate(pts[1].NetTxBps))
	}
	if pts[2].CPUPct != nil || pts[2].MemUsedMB != nil || pts[2].NetRxBps != nil {
		t.Errorf("bucket 2 got %+v, want todo nil", pts[2])
	}
}

func TestMetricSeriesHourlyStep12hAlignedToEpoch(t *testing.T) {
	s := newTestStore(t)
	h := mkHost(t, "srv-12h")
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	insertHourly(t, h, day.Add(11*time.Hour), 60, ptr(10), ptr(1), ptr(1), nil, nil)
	insertHourly(t, h, day.Add(12*time.Hour), 60, ptr(30), ptr(1), ptr(1), nil, nil)

	pts, err := s.MetricSeriesHourly(context.Background(), h, day, 12*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 2 || !pts[0].TS.Equal(day) || !pts[1].TS.Equal(day.Add(12*time.Hour)) {
		t.Fatalf("buckets got %+v, want 00:00 y 12:00 UTC", pts)
	}
	if !near(pts[0].CPUPct, 10) || !near(pts[1].CPUPct, 30) {
		t.Errorf("cpu got %s/%s, want 10/30", fmtRate(pts[0].CPUPct), fmtRate(pts[1].CPUPct))
	}
}

func TestMetricSeriesHourlyEmptyAndUnknownHost(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	h := mkHost(t, "srv-vacio")
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	pts, err := s.MetricSeriesHourly(ctx, h, from, time.Hour)
	if err != nil || pts == nil || len(pts) != 0 {
		t.Errorf("host sin datos got (%v, %v), want lista vacia no nula y sin error", pts, err)
	}
	if _, err := s.MetricSeriesHourly(ctx, "00000000-0000-0000-0000-0000000000ff", from, time.Hour); !errors.Is(err, ErrNotFound) {
		t.Errorf("host inexistente got %v, want ErrNotFound", err)
	}
}

func TestIngestWithoutPartitionIsPartitionMissing(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	h := mkHost(t, "srv-sin-particion")
	now := time.Now().UTC()

	// Dentro de las particiones (legacy + 7 dias adelante): ok.
	if _, err := s.IngestMetrics(ctx, h, Sample{Timestamp: now}); err != nil {
		t.Fatalf("ingest normal: %v", err)
	}
	// Sin particion (mas alla de hoy+8 dias): error de dominio reintentable, sin fila ni cambio de host.
	_, err := s.IngestMetrics(ctx, h, Sample{Timestamp: now.AddDate(0, 0, 30), Services: []ServiceCheck{{Name: "ssh", Status: "up"}}})
	if !errors.Is(err, ErrPartitionMissing) {
		t.Fatalf("err got %v, want ErrPartitionMissing", err)
	}
	if got := count(t, `SELECT count(*) FROM metrics`); got != 1 {
		t.Errorf("filas en metrics got %d, want 1 (la transaccion fallida no deja nada)", got)
	}
}

func TestIngestRealCheckViolationIsNotPartitionMissing(t *testing.T) {
	s := newTestStore(t)
	h := mkHost(t, "srv-check")

	// El CHECK de status de service_checks es un error del dato, con nombre de constraint: no es "sin particion".
	_, err := s.IngestMetrics(context.Background(), h, Sample{
		Timestamp: time.Now().UTC(), Services: []ServiceCheck{{Name: "ssh", Status: "roto"}},
	})
	if err == nil || errors.Is(err, ErrPartitionMissing) {
		t.Fatalf("err got %v, want un error que NO sea ErrPartitionMissing", err)
	}
}

func TestIngestIsIdempotentOnPartitionedTable(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	h := mkHost(t, "srv-dup")
	ts := time.Now().UTC().Add(-time.Minute)

	for range 2 {
		if _, err := s.IngestMetrics(ctx, h, Sample{Timestamp: ts, CPUPct: 5}); err != nil {
			t.Fatalf("ingest: %v", err)
		}
	}
	if got := count(t, `SELECT count(*) FROM metrics WHERE host_id = $1`, h); got != 1 {
		t.Errorf("filas got %d, want 1 (ON CONFLICT (host_id, ts) DO NOTHING sobre la tabla particionada)", got)
	}
}

// La migracion 00006 no copia filas: la tabla vieja pasa a ser la particion legacy. Se prueba con datos
// sobre el esquema 00005, hacia arriba, hacia abajo y otra vez hacia arriba.
func TestMigrate00006KeepsDataThroughUpDownUp(t *testing.T) {
	if testPool == nil {
		t.Skip("ARGOS_TEST_DATABASE_URL no definida")
	}
	ctx := context.Background()
	t.Cleanup(func() {
		if err := resetSchema(ctx); err != nil {
			t.Errorf("restaurando schema: %v", err)
		}
	})
	if err := dropSchema(ctx); err != nil {
		t.Fatal(err)
	}
	provider, err := newProvider(testPool)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 5); err != nil {
		t.Fatalf("up a 00005: %v", err)
	}
	mustExec(t, `INSERT INTO hosts (hostname, agent_token_hash, status) VALUES ('mig-01', 'h1', 'up')`)
	mustExec(t, `INSERT INTO metrics (host_id, ts, cpu_pct, mem_used_mb, mem_total_mb, net_rx_bytes, net_tx_bytes)
	             SELECT id, now() - g * interval '1 hour', g, 100 + g, 1000, g * 1000, g * 10 FROM hosts, generate_series(0, 49) g`)
	mustExec(t, `INSERT INTO service_checks (host_id, name, status, response_ms, ts)
	             SELECT id, 'ssh', 'up', 5, now() - g * interval '1 hour' FROM hosts, generate_series(0, 9) g`)

	check := func(stage string, wantParts int) {
		t.Helper()
		if got := count(t, `SELECT count(*) FROM metrics`); got != 50 {
			t.Errorf("%s: metrics got %d filas, want 50", stage, got)
		}
		if got := count(t, `SELECT count(*) FROM service_checks`); got != 10 {
			t.Errorf("%s: service_checks got %d filas, want 10", stage, got)
		}
		if got := count(t, `SELECT count(*) FROM pg_inherits WHERE inhparent = 'metrics'::regclass`); got != wantParts {
			t.Errorf("%s: particiones de metrics got %d, want %d", stage, got, wantParts)
		}
	}
	check("00005", 0)

	if _, err := provider.UpTo(ctx, 6); err != nil {
		t.Fatalf("up a 00006: %v", err)
	}
	check("tras up", 8) // legacy + 7 diarias
	if got := count(t, `SELECT count(*) FROM ONLY metrics_legacy`); got != 50 {
		t.Errorf("filas en metrics_legacy got %d, want 50 (la conversion no copia: adopta la tabla)", got)
	}
	if got := count(t, `SELECT count(*) FROM ONLY service_checks_legacy`); got != 10 {
		t.Errorf("filas en service_checks_legacy got %d, want 10", got)
	}
	// El CHECK de status de service_checks sobrevive a la conversion y la columna id ya no existe.
	if got := count(t, `SELECT count(*) FROM information_schema.columns WHERE table_name = 'service_checks' AND column_name = 'id'`); got != 0 {
		t.Errorf("service_checks.id sigue existiendo")
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO service_checks (host_id, name, status, ts) SELECT id, 'x', 'roto', now() FROM hosts`); err == nil {
		t.Errorf("el CHECK de status de service_checks se perdio")
	}

	if _, err := provider.DownTo(ctx, 5); err != nil {
		t.Fatalf("down a 00005: %v", err)
	}
	check("tras down", 0)
	if got := count(t, `SELECT count(*) FROM pg_class WHERE relname IN ('metrics_hourly', 'maintenance_state', 'metrics_legacy')`); got != 0 {
		t.Errorf("el down dejo tablas de 00006")
	}

	if _, err := provider.UpTo(ctx, 6); err != nil {
		t.Fatalf("up de nuevo a 00006: %v", err)
	}
	check("tras up de nuevo", 8)
}

// Migraciones 00007-00013 con datos: Up desde 00006 conserva hosts y metricas, 'offline' se admite, y Down a 6
// convierte offline en down sin perder filas; luego Up de nuevo.
func TestMigrateGroupsSchemaWithData(t *testing.T) {
	if testPool == nil {
		t.Skip("ARGOS_TEST_DATABASE_URL no definida")
	}
	ctx := context.Background()
	t.Cleanup(func() {
		if err := resetSchema(ctx); err != nil {
			t.Errorf("restaurando schema: %v", err)
		}
	})
	if err := resetSchema(ctx); err != nil {
		t.Fatal(err)
	}
	provider, err := newProvider(testPool)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.DownTo(ctx, 6); err != nil {
		t.Fatalf("down a 6: %v", err)
	}
	for _, tbl := range []string{"host_groups", "host_tags", "host_info", "disk_samples", "host_latest"} {
		if n := count(t, `SELECT count(*) FROM pg_tables WHERE schemaname = 'public' AND tablename = $1`, tbl); n != 0 {
			t.Errorf("%s sigue existiendo tras down a 6", tbl)
		}
	}
	mustExec(t, `INSERT INTO hosts (hostname, agent_token_hash, status, last_seen_at) VALUES ('a', 'ha', 'up', now()), ('b', 'hb', 'down', now())`)
	mustExec(t, `INSERT INTO metrics (host_id, ts, cpu_pct, mem_used_mb, mem_total_mb) SELECT id, now(), 5, 10, 20 FROM hosts`)
	if _, err := testPool.Exec(ctx, `UPDATE hosts SET status = 'offline' WHERE hostname = 'b'`); err == nil {
		t.Fatalf("antes de 00012 'offline' debe violar hosts_status_check")
	}

	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("up desde 6: %v", err)
	}
	if n := count(t, `SELECT count(*) FROM hosts`) + count(t, `SELECT count(*) FROM metrics`); n != 4 {
		t.Errorf("filas tras up got %d, want 4 (hosts + metricas)", n)
	}
	// host_latest se rellena con lo ultimo de las 24 h.
	if n := count(t, `SELECT count(*) FROM host_latest WHERE cpu_pct = 5 AND mem_total_mb = 20`); n != 2 {
		t.Errorf("host_latest backfill got %d, want 2", n)
	}
	// Nuevas columnas de metrics: NULL en filas viejas y usables.
	mustExec(t, `UPDATE metrics SET swap_used_mb = 3, load1 = 0.5, battery_pct = 50, battery_charging = false`)
	mustExec(t, `UPDATE hosts SET status = 'offline' WHERE hostname = 'b'`)
	// hosts_revoked_disabled_check sigue valiendo.
	if _, err := testPool.Exec(ctx, `UPDATE hosts SET agent_token_revoked_at = now() WHERE hostname = 'a'`); err == nil {
		t.Errorf("hosts_revoked_disabled_check se perdio")
	}
	// Alta en un grupo y borrado bloqueado por la FK RESTRICT.
	mustExec(t, `INSERT INTO host_groups (name) VALUES ('g')`)
	mustExec(t, `UPDATE hosts SET group_id = (SELECT id FROM host_groups) WHERE hostname = 'a'`)
	if _, err := testPool.Exec(ctx, `DELETE FROM host_groups`); err == nil {
		t.Errorf("borrar un grupo con hosts debe fallar")
	}

	if _, err := provider.DownTo(ctx, 6); err != nil {
		t.Fatalf("down a 6 con datos: %v", err)
	}
	if got := count(t, `SELECT count(*) FROM hosts WHERE hostname = 'b' AND status = 'down'`); got != 1 {
		t.Errorf("offline debe volver a down en el Down")
	}
	if n := count(t, `SELECT count(*) FROM hosts`) + count(t, `SELECT count(*) FROM metrics`); n != 4 {
		t.Errorf("filas tras down got %d, want 4", n)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("up de nuevo: %v", err)
	}
}
