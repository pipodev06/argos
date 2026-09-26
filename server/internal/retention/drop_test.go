package retention

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// setThrough fija la marca de agua del resumen (como la deja el rollup).
func setThrough(t *testing.T, at time.Time) {
	t.Helper()
	mustExec(t, `INSERT INTO maintenance_state (name, value_ts) VALUES ($1, $2)
	             ON CONFLICT (name) DO UPDATE SET value_ts = EXCLUDED.value_ts`, rollupMarker, at)
}

// dayOffsets devuelve las particiones diarias de parent como dias de desplazamiento respecto de base. Ignora la
// legacy y las diarias que crea la migracion (100 dias atras respecto del "ahora" simulado de los tests).
func dayOffsets(t *testing.T, parent string, base time.Time) map[int]bool {
	t.Helper()
	got := map[int]bool{}
	for _, n := range partNames(t, parent) {
		suffix, ok := strings.CutPrefix(n, parent+"_p")
		if !ok {
			continue
		}
		day, err := time.Parse("20060102", suffix)
		if err != nil {
			t.Fatalf("nombre de particion inesperado %q", n)
		}
		if off := int(day.Sub(base).Hours() / 24); off > -50 {
			got[off] = true
		}
	}
	return got
}

func wantDays(t *testing.T, what string, got map[int]bool, from, to int) {
	t.Helper()
	for d := from; d <= to; d++ {
		if !got[d] {
			t.Errorf("%s: falta el dia %+d", what, d)
		}
	}
	for d := range got {
		if d < from || d > to {
			t.Errorf("%s: sobra el dia %+d (se esperaba %+d..%+d)", what, d, from, to)
		}
	}
}

func hasLegacy(t *testing.T, parent string) bool {
	t.Helper()
	for _, n := range partNames(t, parent) {
		if n == parent+"_legacy" {
			return true
		}
	}
	return false
}

// Las particiones de metrics solo se borran si estan vencidas (retencion cruda) Y ya resumidas
// (cota superior <= marca de agua). Sin marca de agua no se borra nada.
func TestDropExpiredRespectsRetentionAndWatermark(t *testing.T) {
	fresh(t)
	ctx := context.Background()
	now := farNow()
	today := utcDay(now)
	m, logs := newMaint(t, testCfg, now) // crudo 8 d, service_checks 14 d

	day := func(off int) time.Time { return today.AddDate(0, 0, off) }
	h := mkHost(t, "srv-drop")
	for off := -16; off <= 0; off++ {
		mkPartition(t, tableMetrics, day(off))
		mkPartition(t, tableServiceChecks, day(off))
	}
	// Una fila en una particion que se va a borrar (-13) y otra en una que se queda (-7).
	mustExec(t, `INSERT INTO metrics (host_id, ts, cpu_pct) VALUES ($1, $2, 1), ($1, $3, 2)`, h, day(-13).Add(time.Hour), day(-7).Add(time.Hour))
	mustExec(t, `INSERT INTO service_checks (host_id, name, status, ts) VALUES ($1, 'a', 'up', $2), ($1, 'b', 'up', $3)`, h, day(-15).Add(time.Hour), day(-7).Add(time.Hour))

	drop := func() {
		t.Helper()
		withConn(t, func(conn *pgx.Conn) {
			if err := m.dropExpired(ctx, conn, now); err != nil {
				t.Fatalf("dropExpired: %v", err)
			}
		})
	}

	// 1) Sin marca de agua: metrics intacta (ni siquiera la legacy); service_checks no tiene guardia y si se poda.
	drop()
	wantDays(t, "metrics sin marca", dayOffsets(t, tableMetrics, today), -16, 0)
	if !hasLegacy(t, tableMetrics) {
		t.Errorf("metrics_legacy se borro sin marca de agua")
	}
	// service_checks (14 d): vencidas las que terminan <= hoy-14 -> dias <= -15; la legacy y las de la migracion tambien.
	wantDays(t, "service_checks", dayOffsets(t, tableServiceChecks, today), -14, 0)
	if hasLegacy(t, tableServiceChecks) {
		t.Errorf("service_checks_legacy sigue existiendo pese a estar vencida")
	}
	if got := count(t, `SELECT count(*) FROM service_checks`); got != 1 {
		t.Errorf("filas de service_checks got %d, want 1 (solo la de la particion vigente)", got)
	}

	// 2) Marca de agua atrasada (hoy-12): vencidas por retencion son los dias <= -9, pero solo se van las que
	// terminan <= hoy-12, o sea los dias <= -13 (y la legacy y las de la migracion).
	setThrough(t, day(-12))
	drop()
	wantDays(t, "metrics con marca atrasada", dayOffsets(t, tableMetrics, today), -12, 0)
	if hasLegacy(t, tableMetrics) {
		t.Errorf("metrics_legacy sigue existiendo pese a estar vencida y resumida")
	}
	if got := count(t, `SELECT count(*) FROM metrics`); got != 1 {
		t.Errorf("filas de metrics got %d, want 1 (la de -13 se borro con su particion, la de -7 sigue)", got)
	}

	// 3) Marca de agua al dia: se borran hasta el limite de la retencion (terminan <= hoy-8 -> dias <= -9).
	setThrough(t, day(0))
	drop()
	wantDays(t, "metrics con marca al dia", dayOffsets(t, tableMetrics, today), -8, 0)

	// 4) Idempotente: repetir no cambia nada ni falla.
	drop()
	wantDays(t, "metrics repetido", dayOffsets(t, tableMetrics, today), -8, 0)
	if got := count(t, `SELECT count(*) FROM metrics`); got != 1 {
		t.Errorf("filas de metrics tras repetir got %d, want 1", got)
	}
	logs.noErrors(t)
}

// Una marca de agua a mitad de dia no libera la particion de ese dia hasta que la pase entera.
func TestDropExpiredKeepsPartitionUntilWatermarkPassesItsUpperBound(t *testing.T) {
	fresh(t)
	ctx := context.Background()
	now := farNow()
	today := utcDay(now)
	m, logs := newMaint(t, testCfg, now)
	old := today.AddDate(0, 0, -20)
	mkPartition(t, tableMetrics, old)

	drop := func(through time.Time) {
		t.Helper()
		setThrough(t, through)
		withConn(t, func(conn *pgx.Conn) {
			if err := m.dropExpired(ctx, conn, now); err != nil {
				t.Fatal(err)
			}
		})
	}
	drop(old.AddDate(0, 0, 1).Add(-time.Hour)) // 23:00 del mismo dia: aun le faltan datos por resumir
	if !hasPartition(t, tableMetrics, old) {
		t.Fatalf("la particion se borro con la marca de agua una hora antes de su cota superior")
	}
	drop(old.AddDate(0, 0, 1)) // justo su cota superior
	if hasPartition(t, tableMetrics, old) {
		t.Errorf("la particion sigue existiendo con la marca de agua en su cota superior")
	}
	logs.noErrors(t)
}

// Un DETACH CONCURRENTLY interrumpido (lock_timeout esperando a una consulta) deja la particion en
// "detach pendiente"; el ciclo siguiente lo finaliza y la borra. Con un lector activo se reintenta.
func TestDropExpiredRecoversPendingDetach(t *testing.T) {
	fresh(t)
	ctx := context.Background()
	now := farNow()
	today := utcDay(now)
	m, logs := newMaint(t, testCfg, now)

	old := today.AddDate(0, 0, -12)
	name := partitionName(tableMetrics, old)
	mkPartition(t, tableMetrics, old)
	setThrough(t, today)

	// Un lector mantiene abierta la particion vencida (la legacy y las de la migracion, tambien vencidas, se borran normal).
	reader, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Rollback(ctx) }() // idempotente: el test ya la cierra explicitamente
	if _, err := reader.Exec(ctx, `SELECT count(*) FROM `+name); err != nil {
		t.Fatal(err)
	}

	withConn(t, func(conn *pgx.Conn) {
		if _, err := conn.Exec(ctx, `SET lock_timeout = 300`); err != nil {
			t.Fatal(err)
		}
		err := m.dropExpired(ctx, conn, now)
		if err == nil || !strings.Contains(err.Error(), "desacoplando") {
			t.Fatalf("dropExpired con un lector activo got %v, want un error de DETACH", err)
		}
		if _, err := conn.Exec(ctx, `RESET lock_timeout`); err != nil {
			t.Fatal(err)
		}
	})
	if got := count(t, `SELECT count(*) FROM pg_inherits WHERE inhparent = 'metrics'::regclass AND inhrelid = $1::regclass AND inhdetachpending`, name); got != 1 {
		t.Fatalf("la particion deberia quedar con DETACH pendiente (got %d)", got)
	}

	// Sin el lector, el siguiente ciclo finaliza el DETACH y borra la particion.
	if err := reader.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	logs.reset()
	withConn(t, func(conn *pgx.Conn) {
		if err := m.dropExpired(ctx, conn, now); err != nil {
			t.Fatalf("recuperacion: %v", err)
		}
	})
	if got := count(t, `SELECT count(*) FROM pg_class WHERE relname = $1`, name); got != 0 {
		t.Errorf("la tabla %s sigue existiendo tras finalizar el DETACH", name)
	}
	if !strings.Contains(logs.String(), "DETACH pendiente finalizado") {
		t.Errorf("no se logueo la finalizacion del DETACH pendiente: %s", logs.String())
	}
	logs.noErrors(t)
}

// Tablas metrics_pYYYYMMDD sueltas (un DETACH cuyo DROP fallo) se limpian; las que aun estarian en
// ventana de retencion se conservan.
func TestDropOrphans(t *testing.T) {
	fresh(t)
	ctx := context.Background()
	now := farNow()
	today := utcDay(now)
	m, logs := newMaint(t, testCfg, now)
	setThrough(t, today)

	oldOrphan := partitionName(tableMetrics, today.AddDate(0, 0, -20))
	freshOrphan := partitionName(tableMetrics, today.AddDate(0, 0, -2))
	for _, name := range []string{oldOrphan, freshOrphan} {
		mustExec(t, `CREATE TABLE `+name+` (LIKE metrics INCLUDING DEFAULTS)`)
	}
	mustExec(t, `CREATE TABLE unrelated_table (id int)`)

	withConn(t, func(conn *pgx.Conn) {
		if err := m.dropExpired(ctx, conn, now); err != nil {
			t.Fatal(err)
		}
	})
	if got := count(t, `SELECT count(*) FROM pg_class WHERE relname = $1`, oldOrphan); got != 0 {
		t.Errorf("la huerfana vencida %s no se borro", oldOrphan)
	}
	if got := count(t, `SELECT count(*) FROM pg_class WHERE relname = $1`, freshOrphan); got != 1 {
		t.Errorf("la huerfana vigente %s se borro", freshOrphan)
	}
	if got := count(t, `SELECT count(*) FROM pg_class WHERE relname = 'unrelated_table'`); got != 1 {
		t.Errorf("se borro una tabla ajena")
	}
	if !strings.Contains(logs.String(), "huerfana fuera de la ventana") {
		t.Errorf("no se aviso de la huerfana conservada: %s", logs.String())
	}
}

func TestPurgeHourlyDeletesOnlyExpiredInBatches(t *testing.T) {
	fresh(t)
	ctx := context.Background()
	now := farNow()
	today := utcDay(now)
	m, logs := newMaint(t, Config{HotDays: 8, DetailDays: 30, HourlyDays: 10, ServiceChecksDays: 14, DiskDays: 30, Interval: time.Minute}, now)

	h := mkHost(t, "srv-hourly-purge")
	// Mas filas vencidas que el tamano del lote (5000), pa recorrer varios lotes: 6500 horas ~ 270 dias.
	mustExec(t, `INSERT INTO metrics_hourly (host_id, hour, samples)
	             SELECT $1, $2::timestamptz - g * interval '1 hour', 1 FROM generate_series(1, 6500) g`, h, today.AddDate(0, 0, -10))
	// Vigentes: desde hoy-10 (inclusive) hasta hoy.
	mustExec(t, `INSERT INTO metrics_hourly (host_id, hour, samples)
	             SELECT $1, $2::timestamptz + g * interval '1 hour', 1 FROM generate_series(0, 240) g`, h, today.AddDate(0, 0, -10))

	withConn(t, func(conn *pgx.Conn) {
		if err := m.purgeHourly(ctx, conn, now); err != nil {
			t.Fatal(err)
		}
	})
	if got := count(t, `SELECT count(*) FROM metrics_hourly WHERE hour < $1`, today.AddDate(0, 0, -10)); got != 0 {
		t.Errorf("quedaron %d resumenes vencidos", got)
	}
	if got := count(t, `SELECT count(*) FROM metrics_hourly`); got != 241 {
		t.Errorf("resumenes vigentes got %d, want 241", got)
	}
	logs.noErrors(t)
}
