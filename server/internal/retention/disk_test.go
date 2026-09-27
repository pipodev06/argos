package retention

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"argos/server/internal/chunk"
	"argos/server/internal/store"
)

// disk_samples se poda con una ventana caliente fija (diskHotDays, sin marca de agua ni resumen): vencidas las
// que terminan <= hoy - diskHotDays, incluida la legacy; las vigentes conservan sus filas. Antes de borrarse cada
// host-dia se empaqueta en disk_chunks (con budget alto para no depender del limite por ciclo aqui).
func TestDropExpiredDiskSamples(t *testing.T) {
	fresh(t)
	ctx := context.Background()
	now := farNow()
	today := utcDay(now)
	m, logs := newMaint(t, testCfg, now)
	m.packBudget = 100
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
	// Ventana caliente diskHotDays (8): se van los dias cuya cota superior es <= hoy-8, o sea los dias <= -9.
	wantDays(t, "disk_samples", dayOffsets(t, tableDiskSamples, today), -8, 0)
	if hasLegacy(t, tableDiskSamples) {
		t.Errorf("disk_samples_legacy sigue existiendo pese a estar vencida")
	}
	if got := count(t, `SELECT count(*) FROM disk_samples`); got != 1 {
		t.Errorf("filas de disk_samples got %d, want 1 (la de -12 se fue con su particion, empaquetada)", got)
	}
	if got := count(t, `SELECT count(*) FROM disk_chunks WHERE host_id = $1 AND day = $2::date AND n = 1`, h, day(-12)); got != 1 {
		t.Errorf("el dia -12 debia quedar empaquetado en disk_chunks, got %d", got)
	}
	logs.noErrors(t)
}

// diskDetail lee todo el detalle de disco de un host entre from y to con Store.DiskDetailRange.
func diskDetail(t *testing.T, hostID string, from, to time.Time) []chunk.DiskRow {
	t.Helper()
	var got []chunk.DiskRow
	err := store.New(testPool).DiskDetailRange(context.Background(), hostID, from, to, func(r store.DiskDetailSample) error {
		got = append(got, r)
		return nil
	})
	if err != nil {
		t.Fatalf("DiskDetailRange: %v", err)
	}
	return got
}

// Empaquetar disco y borrar la particion: DiskDetailRange devuelve las mismas filas, mezclando montajes.
func TestPackDiskThenDropKeepsDetail(t *testing.T) {
	fresh(t)
	ctx := context.Background()
	now := farNow()
	today := utcDay(now)
	m, logs := newMaint(t, testCfg, now)
	m.packBudget = 100
	h := mkHost(t, "srv-disco-empaque")

	d := today.AddDate(0, 0, -12)
	mkPartition(t, tableDiskSamples, d)
	want := []chunk.DiskRow{
		{TS: d.Add(time.Hour), Mount: "/", UsedMB: 1000, TotalMB: 500000},
		{TS: d.Add(time.Hour), Mount: "/data", UsedMB: 2000, TotalMB: 1000000},
		{TS: d.Add(2 * time.Hour), Mount: "/", UsedMB: 1010, TotalMB: 500000},
		{TS: d.Add(2 * time.Hour), Mount: "/data", UsedMB: 2050, TotalMB: 1000000},
	}
	for _, r := range want {
		mustExec(t, `INSERT INTO disk_samples (host_id, ts, used_mb, total_mb, mount) VALUES ($1, $2, $3, $4, $5)`,
			h, r.TS, r.UsedMB, r.TotalMB, r.Mount)
	}

	if !m.RunOnce(ctx) {
		t.Fatal("RunOnce got ran=false")
	}
	logs.noErrors(t)
	if hasPartition(t, tableDiskSamples, d) {
		t.Fatal("la particion de disco vencida y empaquetada debia borrarse")
	}
	if got := count(t, `SELECT count(*) FROM disk_chunks WHERE host_id = $1 AND day = $2::date AND n = $3 AND mount_count = 2`, h, d, len(want)); got != 1 {
		t.Fatalf("chunks de disco del host-dia = %d, want 1", got)
	}
	got := diskDetail(t, h, d, d.AddDate(0, 0, 1))
	if len(got) != len(want) {
		t.Fatalf("DiskDetailRange devolvio %d filas, want %d", len(got), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("fila %d:\n got  %+v\n want %+v", i, got[i], want[i])
		}
	}
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
