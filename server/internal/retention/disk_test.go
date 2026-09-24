package retention

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// disk_samples se poda por dias (sin marca de agua ni resumen): vencidas las que terminan <= hoy - DiskDays,
// incluida la legacy; las vigentes conservan sus filas.
func TestDropExpiredDiskSamples(t *testing.T) {
	fresh(t)
	ctx := context.Background()
	now := farNow()
	today := utcDay(now)
	cfg := testCfg
	cfg.DiskDays = 10
	m, logs := newMaint(t, cfg, now)
	day := func(off int) time.Time { return today.AddDate(0, 0, off) }

	h := mkHost(t, "srv-disk")
	for off := -14; off <= 0; off++ {
		mkPartition(t, tableDiskSamples, day(off))
	}
	mustExec(t, `INSERT INTO disk_samples (ts, host_id, used_mb, total_mb, mount) VALUES ($2, $1, 1, 2, '/'), ($3, $1, 1, 2, '/')`,
		h, day(-12).Add(time.Hour), day(-5).Add(time.Hour))

	withConn(t, func(conn *pgx.Conn) {
		if err := m.dropExpired(ctx, conn, now); err != nil {
			t.Fatalf("dropExpired: %v", err)
		}
	})
	// Retencion 10 d: se van los dias cuya cota superior es <= hoy-10, o sea los dias <= -11.
	wantDays(t, "disk_samples", dayOffsets(t, tableDiskSamples, today), -10, 0)
	if hasLegacy(t, tableDiskSamples) {
		t.Errorf("disk_samples_legacy sigue existiendo pese a estar vencida")
	}
	if got := count(t, `SELECT count(*) FROM disk_samples`); got != 1 {
		t.Errorf("filas de disk_samples got %d, want 1 (la de -12 se fue con su particion)", got)
	}
	logs.noErrors(t)
}

// Un ciclo completo crea las particiones de disk_samples hoy..hoy+7.
func TestRunOnceCreatesDiskPartitions(t *testing.T) {
	fresh(t)
	now := farNow()
	m, logs := newMaint(t, testCfg, now)
	if !m.RunOnce(context.Background()) {
		t.Fatal("RunOnce got ran=false")
	}
	for off := 0; off <= createAheadDays; off++ {
		if !hasPartition(t, tableDiskSamples, utcDay(now).AddDate(0, 0, off)) {
			t.Errorf("falta la particion de disk_samples de hoy+%d", off)
		}
	}
	logs.noErrors(t)
}

// El resumen por hora agrega swap, carga y bateria (promedio/maximo/minimo) y una segunda corrida no reescribe nada.
func TestRollupNewColumns(t *testing.T) {
	fresh(t)
	now := farNow()
	m, logs := newMaint(t, testCfg, now)
	h := mkHost(t, "srv-nuevas")
	hour := utcDay(now).AddDate(0, 0, -1).Add(3 * time.Hour)
	mkPartition(t, tableMetrics, utcDay(hour))

	// 4 muestras: swap 100,200,300,400; load1 1,2,3,4 (load5/15 constantes); bateria 80,70,60,50; una sin datos nuevos.
	for i := range 4 {
		mustExec(t, `INSERT INTO metrics (host_id, ts, cpu_pct, mem_used_mb, mem_total_mb, swap_used_mb, swap_total_mb, load1, load5, load15, battery_pct)
		             VALUES ($1, $2, 1, 1, 2, $3, 1000, $4, 0.5, 0.25, $5)`,
			h, hour.Add(time.Duration(i)*time.Minute), 100*(i+1), float64(i+1), 80-10*i)
	}
	mustExec(t, `INSERT INTO metrics (host_id, ts, cpu_pct, mem_used_mb, mem_total_mb) VALUES ($1, $2, 1, 1, 2)`, h, hour.Add(10*time.Minute))

	// Un host de agente viejo en la misma hora: sin columnas nuevas.
	h2 := mkHost(t, "srv-viejo")
	insertRows(t, h2, minuteSeries(hour, 3, nil, func(int) float64 { return 1 }, func(int) int { return 1 }, nil, nil))

	runRollup(t, m, now)
	var swapAvg, swapMax, swapTot int
	var l1avg, l1max, l5, l15, battAvg float64
	var battMin int
	err := testPool.QueryRow(context.Background(),
		`SELECT swap_used_avg_mb, swap_used_max_mb, swap_total_mb, load1_avg, load1_max, load5_avg, load15_avg, battery_pct_avg, battery_pct_min
		   FROM metrics_hourly WHERE host_id = $1 AND hour = $2`, h, hour).
		Scan(&swapAvg, &swapMax, &swapTot, &l1avg, &l1max, &l5, &l15, &battAvg, &battMin)
	if err != nil {
		t.Fatal(err)
	}
	if swapAvg != 250 || swapMax != 400 || swapTot != 1000 || l1avg != 2.5 || l1max != 4 || l5 != 0.5 || l15 != 0.25 || battAvg != 65 || battMin != 50 {
		t.Errorf("resumen got swap %d/%d/%d load1 %v/%v load5 %v load15 %v bateria %v/%d", swapAvg, swapMax, swapTot, l1avg, l1max, l5, l15, battAvg, battMin)
	}

	// Sin ninguna columna nueva quedan NULL (no cero).
	if n := count(t, `SELECT count(*) FROM metrics_hourly WHERE host_id = $1 AND swap_used_avg_mb IS NULL AND load1_avg IS NULL AND battery_pct_avg IS NULL`, h2); n != 1 {
		t.Errorf("host sin columnas nuevas got %d filas con NULL, want 1", n)
	}

	// Idempotente: no reescribe (xmin estable).
	var x1, x2 string
	q := `SELECT xmin::text FROM metrics_hourly WHERE host_id = $1 AND hour = $2`
	if err := testPool.QueryRow(context.Background(), q, h, hour).Scan(&x1); err != nil {
		t.Fatal(err)
	}
	runRollup(t, m, now)
	if err := testPool.QueryRow(context.Background(), q, h, hour).Scan(&x2); err != nil || x1 != x2 {
		t.Errorf("una segunda corrida no debe reescribir: xmin %s -> %s (%v)", x1, x2, err)
	}
	logs.noErrors(t)
}
