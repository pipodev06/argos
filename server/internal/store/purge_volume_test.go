package store

import (
	"context"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestPurgeVolume mide la purga de un host con mucho historial: la forma anterior (una sola transaccion, purgeHostTx)
// contra la actual (por tandas de un dia, PurgeHost). Solo corre con ARGOS_VOLUME_ROWS=<filas por host>; no forma parte
// de la suite normal. Lo orquesta scripts/perf/purge-volume.sh sobre una BD desechable.
func TestPurgeVolume(t *testing.T) {
	raw := os.Getenv("ARGOS_VOLUME_ROWS")
	if raw == "" {
		t.Skip("ARGOS_VOLUME_ROWS no definida (medicion manual: scripts/perf/purge-volume.sh)")
	}
	rows, err := strconv.Atoi(raw)
	if err != nil || rows < 1000 {
		t.Fatalf("ARGOS_VOLUME_ROWS debe ser un entero >= 1000, got %q", raw)
	}
	s := newTestStore(t)
	ctx := context.Background()

	// Reparto por host: 70 % chequeos, 20 % metricas, 10 % discos, repartidos en 6 dias (particiones existentes).
	nChecks, nMetrics, nDisks := rows*70/100, rows*20/100, rows*10/100
	load := func(name string) string {
		seedHost(t, name, "up", ago(time.Second))
		id := hostID(t, name)
		t0 := time.Now()
		spread := func(n int) string {
			return "now() - (i::float8 * (518400.0 / " + strconv.Itoa(n) + ")) * interval '1 second'"
		}
		mustExec(t, `INSERT INTO service_checks (host_id, name, status, response_ms, ts)
		             SELECT $1, 'c' || (i % 20), 'up', 3, `+spread(nChecks)+` FROM generate_series(1, `+strconv.Itoa(nChecks)+`) i`, id)
		mustExec(t, `INSERT INTO metrics (host_id, ts, cpu_pct) SELECT $1, `+spread(nMetrics)+`, 1 FROM generate_series(1, `+strconv.Itoa(nMetrics)+`) i`, id)
		mustExec(t, `INSERT INTO disk_samples (ts, host_id, used_mb, total_mb, mount)
		             SELECT `+spread(nDisks)+`, $1, 100, 1000, 'm' || (i % 3) FROM generate_series(1, `+strconv.Itoa(nDisks)+`) i`, id)
		t.Logf("carga de %s: %d filas en %s", name, rows, time.Since(t0).Round(time.Second))
		return id
	}

	// measure corre fn y devuelve duracion, WAL generado y la transaccion mas larga vista desde otra conexion.
	measure := func(label string, fn func() error) {
		var lsn0 string
		if err := testPool.QueryRow(ctx, `SELECT pg_current_wal_lsn()::text`).Scan(&lsn0); err != nil {
			t.Fatal(err)
		}
		var (
			mu      sync.Mutex
			longest float64
		)
		stop := make(chan struct{})
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				select {
				case <-stop:
					return
				case <-time.After(50 * time.Millisecond):
				}
				var age float64
				if err := testPool.QueryRow(ctx, `SELECT coalesce(max(extract(epoch FROM now() - xact_start)), 0)::float8
				    FROM pg_stat_activity WHERE datname = current_database() AND backend_type = 'client backend'
				      AND pid <> pg_backend_pid() AND xact_start IS NOT NULL AND state <> 'idle'`).Scan(&age); err == nil {
					mu.Lock()
					longest = max(longest, age)
					mu.Unlock()
				}
			}
		}()
		t0 := time.Now()
		err := fn()
		took := time.Since(t0)
		close(stop)
		<-done
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		var walMB float64
		if err := testPool.QueryRow(ctx, `SELECT pg_wal_lsn_diff(pg_current_wal_lsn(), $1::pg_lsn) / 1048576.0`, lsn0).Scan(&walMB); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		t.Logf("MEDIDA %-28s tiempo=%-8s wal=%7.0f MB  transaccion_mas_larga=%.1f s", label, took.Round(10*time.Millisecond), walMB, longest)
		mu.Unlock()
	}

	single := ""
	if os.Getenv("ARGOS_VOLUME_SKIP_BASELINE") == "" {
		single = load("volumen-una-tx")
		measure("una sola transaccion", func() error {
			return pgx.BeginFunc(ctx, testPool, func(tx pgx.Tx) error {
				_, err := purgeHostTx(ctx, tx, single, nil)
				return err
			})
		})
	}
	batched := load("volumen-por-tandas")
	measure("por tandas de un dia", func() error {
		_, err := s.PurgeHost(ctx, batched, "volumen-por-tandas")
		return err
	})

	for _, id := range []string{single, batched} {
		if id == "" {
			continue
		}
		if n := count(t, `SELECT (SELECT count(*) FROM metrics WHERE host_id = $1) + (SELECT count(*) FROM service_checks WHERE host_id = $1)
		                       + (SELECT count(*) FROM disk_samples WHERE host_id = $1) + (SELECT count(*) FROM hosts WHERE id = $1)`, id); n != 0 {
			t.Errorf("quedaron %d filas del host %s", n, id)
		}
	}
}

// TestPurgeConcurrentVolume: 10 bajas de hosts distintos a la vez. Mide cuanto tarda el total y, sobre todo, cuanto
// tarda una consulta trivial (lo que veria el ingest de un agente o el dashboard) mientras se purga: con el tope de
// bajas simultaneas (maxConcurrentPurges) contra sin tope (10 slots = las 10 conexiones del pool).
func TestPurgeConcurrentVolume(t *testing.T) {
	raw := os.Getenv("ARGOS_VOLUME_ROWS")
	if raw == "" {
		t.Skip("ARGOS_VOLUME_ROWS no definida (medicion manual: scripts/perf/purge-volume.sh)")
	}
	perHost, err := strconv.Atoi(raw)
	if err != nil || perHost < 1000 {
		t.Fatalf("ARGOS_VOLUME_ROWS debe ser un entero >= 1000, got %q", raw)
	}
	perHost /= 10 // el mismo volumen total repartido en 10 hosts
	const hosts = 10
	ctx := context.Background()

	run := func(label string, slots int) {
		s := newTestStore(t)
		s.purgeSlots = make(chan struct{}, slots)
		names := make([]string, hosts)
		for i := range names {
			names[i] = "conc-" + strconv.Itoa(i)
			seedHost(t, names[i], "up", ago(time.Second))
			id := hostID(t, names[i])
			n := strconv.Itoa(perHost)
			mustExec(t, `INSERT INTO service_checks (host_id, name, status, response_ms, ts)
			             SELECT $1, 'c' || (i % 20), 'up', 3, now() - (i::float8 * (518400.0 / `+n+`)) * interval '1 second' FROM generate_series(1, `+n+`) i`, id)
			mustExec(t, `INSERT INTO metrics (host_id, ts, cpu_pct)
			             SELECT $1, now() - (i::float8 * (518400.0 / `+n+`)) * interval '1 second', 1 FROM generate_series(1, `+n+`) i`, id)
		}

		var (
			mu     sync.Mutex
			worst  time.Duration
			probes int
		)
		stop, done := make(chan struct{}), make(chan struct{})
		go func() {
			defer close(done)
			for {
				select {
				case <-stop:
					return
				case <-time.After(20 * time.Millisecond):
				}
				t0 := time.Now()
				var one int
				if err := s.pool.QueryRow(ctx, `SELECT 1`).Scan(&one); err == nil {
					d := time.Since(t0)
					mu.Lock()
					worst = max(worst, d)
					probes++
					mu.Unlock()
				}
			}
		}()

		t0 := time.Now()
		var wg sync.WaitGroup
		errs := make(chan error, hosts)
		for _, name := range names {
			id := hostID(t, name)
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := s.PurgeHost(ctx, id, name); err != nil {
					errs <- err
				}
			}()
		}
		wg.Wait()
		took := time.Since(t0)
		close(stop)
		<-done
		close(errs)
		for err := range errs {
			t.Errorf("%s: %v", label, err)
		}
		if n := count(t, `SELECT count(*) FROM hosts WHERE hostname LIKE 'conc-%'`); n != 0 {
			t.Errorf("%s: quedaron %d hosts", label, n)
		}
		mu.Lock()
		t.Logf("MEDIDA %-32s total=%-8s consulta_trivial_peor=%-8s (%d sondeos)", label, took.Round(10*time.Millisecond), worst.Round(time.Millisecond), probes)
		mu.Unlock()
	}

	run("10 bajas, sin tope (10 a la vez)", hosts)
	run("10 bajas, tope de "+strconv.Itoa(maxConcurrentPurges)+" a la vez", maxConcurrentPurges)
}
