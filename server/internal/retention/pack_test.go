package retention

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"argos/server/internal/chunk"
	"argos/server/internal/store"
)

var (
	fp32 = func(v float32) *float32 { return &v }
	ip32 = func(v int32) *int32 { return &v }
)

// fullRows arma n muestras cada 30 s desde start con todas las columnas (algunas NULL, un reinicio de contador).
func fullRows(start time.Time, n int) []chunk.Row {
	rows := make([]chunk.Row, 0, n)
	var rx int64 = 1_000_000
	for i := range n {
		r := chunk.Row{TS: start.Add(time.Duration(i) * 30 * time.Second)}
		if i%7 != 3 {
			r.CPUPct = fp32(12.345 + float32(i)*0.137)
		}
		r.MemUsedMB, r.MemTotalMB = ip32(int32(4000+i)), ip32(16000)
		rx += int64(i * 1500)
		if i == n/2 {
			rx = 10 // reinicio de contador
		}
		tx := rx / 3
		r.NetRxBytes, r.NetTxBytes = &rx, &tx
		if i%2 == 0 {
			r.Load1, r.Load5, r.Load15 = fp32(0.5+float32(i)/100), fp32(0.4), fp32(0.3)
		}
		if i%10 == 0 {
			r.SwapUsedMB, r.SwapTotalMB = ip32(int32(i)), ip32(2048)
		}
		if i%5 == 0 {
			p, c := int16(90-i%40), i%2 == 0
			r.BatteryPct, r.BatteryCharging = &p, &c
		}
		rows = append(rows, r)
	}
	return rows
}

// insertFull inserta filas de metrics con todas las columnas.
func insertFull(t *testing.T, pool *pgxpool.Pool, hostID string, rows []chunk.Row) {
	t.Helper()
	data := make([][]any, 0, len(rows))
	for _, r := range rows {
		data = append(data, []any{hostID, r.TS, r.CPUPct, r.MemUsedMB, r.MemTotalMB, r.NetRxBytes, r.NetTxBytes,
			r.SwapUsedMB, r.SwapTotalMB, r.Load1, r.Load5, r.Load15, r.BatteryPct, r.BatteryCharging})
	}
	_, err := pool.CopyFrom(context.Background(), pgx.Identifier{"metrics"},
		[]string{"host_id", "ts", "cpu_pct", "mem_used_mb", "mem_total_mb", "net_rx_bytes", "net_tx_bytes", "swap_used_mb", "swap_total_mb",
			"load1", "load5", "load15", "battery_pct", "battery_charging"}, pgx.CopyFromRows(data))
	if err != nil {
		t.Fatalf("insertando metricas completas: %v", err)
	}
}

// rounded devuelve las filas tal como vuelven de un chunk (floats a 2 decimales).
func rounded(rows []chunk.Row) []chunk.Row {
	out := make([]chunk.Row, len(rows))
	for i, r := range rows {
		for _, p := range []**float32{&r.CPUPct, &r.Load1, &r.Load5, &r.Load15} {
			if *p != nil {
				*p = fp32(chunk.Round2(**p))
			}
		}
		out[i] = r
	}
	return out
}

// detail lee todo el detalle de un host entre from y to con Store.DetailRange.
func detail(t *testing.T, hostID string, from, to time.Time) []chunk.Row {
	t.Helper()
	var got []chunk.Row
	err := store.New(testPool).DetailRange(context.Background(), hostID, from, to, func(r store.DetailSample) error {
		got = append(got, r)
		return nil
	})
	if err != nil {
		t.Fatalf("DetailRange: %v", err)
	}
	return got
}

func sameRows(t *testing.T, got, want []chunk.Row) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("DetailRange devolvio %d filas, want %d", len(got), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Fatalf("fila %d:\n got  %+v\n want %+v", i, got[i], want[i])
		}
	}
}

// Empaquetar y borrar la particion: DetailRange devuelve las mismas filas (con el redondeo documentado).
func TestPackThenDropKeepsDetail(t *testing.T) {
	fresh(t)
	ctx := context.Background()
	now := farNow()
	today := utcDay(now)
	m, logs := newMaint(t, testCfg, now)
	h := mkHost(t, "srv-empaque")

	d := today.AddDate(0, 0, -12)
	mkPartition(t, tableMetrics, d)
	rows := fullRows(d.Add(time.Hour), 60)
	insertFull(t, testPool, h, rows)

	if !m.RunOnce(ctx) {
		t.Fatal("RunOnce got ran=false")
	}
	logs.noErrors(t)
	if hasPartition(t, tableMetrics, d) {
		t.Fatal("la particion vencida y empaquetada debia borrarse")
	}
	if got := count(t, `SELECT count(*) FROM metrics_chunks WHERE host_id = $1 AND day = $2::date AND n = $3 AND codec = $4`, h, d, len(rows), chunk.Version); got != 1 {
		t.Fatalf("chunks del host-dia = %d, want 1", got)
	}
	sameRows(t, detail(t, h, d, d.AddDate(0, 0, 1)), rounded(rows))

	// Un subrango parte el chunk por tiempo: [from, to).
	sub := detail(t, h, rows[10].TS, rows[20].TS)
	sameRows(t, sub, rounded(rows[10:20]))
}

// DetailRange mezcla dias empaquetados y crudos, en orden y sin repetir.
func TestDetailRangeMixesChunksAndRaw(t *testing.T) {
	fresh(t)
	ctx := context.Background()
	now := farNow()
	today := utcDay(now)
	m, logs := newMaint(t, testCfg, now)
	h := mkHost(t, "srv-mixto")

	old, hot1, hot2 := today.AddDate(0, 0, -12), today.AddDate(0, 0, -3), today.AddDate(0, 0, -1)
	mkPartition(t, tableMetrics, old)
	mkPartition(t, tableMetrics, hot1)
	mkPartition(t, tableMetrics, hot2)
	oldRows, hot1Rows, hot2Rows := fullRows(old.Add(23*time.Hour+58*time.Minute+30*time.Second), 3), fullRows(hot1.Add(time.Hour), 5), fullRows(hot2.Add(time.Hour), 5)
	insertFull(t, testPool, h, oldRows)
	insertFull(t, testPool, h, hot1Rows)
	insertFull(t, testPool, h, hot2Rows)
	m.RunOnce(ctx)
	logs.noErrors(t)

	if hasPartition(t, tableMetrics, old) || !hasPartition(t, tableMetrics, hot1) {
		t.Fatalf("solo la particion vencida debia borrarse (old=%v hot1=%v)", hasPartition(t, tableMetrics, old), hasPartition(t, tableMetrics, hot1))
	}
	want := append(append(rounded(oldRows), hot1Rows...), hot2Rows...)
	sameRows(t, detail(t, h, old, today), want)
	if got := detail(t, h, today, today.AddDate(0, 0, 1)); len(got) != 0 {
		t.Errorf("hoy no tiene muestras, got %d", len(got))
	}
}

// Empaquetar dos veces no duplica ni cambia nada, y una particion ya empaquetada no gasta presupuesto.
func TestPackIsIdempotent(t *testing.T) {
	fresh(t)
	ctx := context.Background()
	now := farNow()
	today := utcDay(now)
	m, _ := newMaint(t, testCfg, now)
	h := mkHost(t, "srv-idem")
	d := today.AddDate(0, 0, -12)
	mkPartition(t, tableMetrics, d)
	insertFull(t, testPool, h, fullRows(d.Add(time.Hour), 30))

	through := today
	policy := dropPolicy{table: tableMetrics, cutoff: today.AddDate(0, 0, -8), through: &through}
	pack := func() map[string]bool {
		var unpacked map[string]bool
		withConn(t, func(conn *pgx.Conn) {
			var err error
			if unpacked, err = m.packExpired(ctx, conn, policy); err != nil {
				t.Fatalf("packExpired: %v", err)
			}
		})
		return unpacked
	}
	if u := pack(); len(u) != 0 {
		t.Fatalf("sin trabajo pendiente tras empaquetar, got %v", u)
	}
	var packedAt time.Time
	if err := testPool.QueryRow(ctx, `SELECT packed_at FROM metrics_chunks WHERE host_id = $1`, h).Scan(&packedAt); err != nil {
		t.Fatal(err)
	}
	pack()
	var again time.Time
	if err := testPool.QueryRow(ctx, `SELECT packed_at FROM metrics_chunks WHERE host_id = $1`, h).Scan(&again); err != nil {
		t.Fatal(err)
	}
	if !again.Equal(packedAt) || count(t, `SELECT count(*) FROM metrics_chunks`) != 1 {
		t.Errorf("empaquetar de nuevo debia ser un no-op (packed_at %v -> %v)", packedAt, again)
	}

	// Un chunk con n distinto de las filas crudas se rehace (upsert): la particion cruda manda mientras exista.
	mustExec(t, `UPDATE metrics_chunks SET n = 1 WHERE host_id = $1`, h)
	pack()
	if got := count(t, `SELECT count(*) FROM metrics_chunks WHERE host_id = $1 AND n = 30`, h); got != 1 {
		t.Errorf("el chunk desactualizado debia rehacerse, filas con n=30: %d", got)
	}
}

// Si un host no se puede empaquetar, su particion NO se borra (los demas hosts si quedan empaquetados) y el siguiente
// ciclo lo completa cuando el fallo desaparece.
func TestPartialPackFailureKeepsPartition(t *testing.T) {
	fresh(t)
	ctx := context.Background()
	now := farNow()
	today := utcDay(now)
	m, logs := newMaint(t, testCfg, now)
	good, bad := mkHost(t, "srv-bueno"), mkHost(t, "srv-malo")
	d := today.AddDate(0, 0, -12)
	mkPartition(t, tableMetrics, d)
	insertFull(t, testPool, good, fullRows(d.Add(time.Hour), 20))
	insertFull(t, testPool, bad, fullRows(d.Add(2*time.Hour), 20))

	mustExec(t, `CREATE FUNCTION zz_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fallo simulado'; END $$`)
	mustExec(t, `CREATE TRIGGER zz_fail BEFORE INSERT OR UPDATE ON metrics_chunks FOR EACH ROW WHEN (NEW.host_id = '`+bad+`') EXECUTE FUNCTION zz_fail()`)

	m.RunOnce(ctx)
	if !hasPartition(t, tableMetrics, d) {
		t.Fatal("la particion con un host sin empaquetar no debe borrarse")
	}
	if !strings.Contains(logs.String(), `"level":"ERROR"`) || !strings.Contains(logs.String(), "fallo simulado") {
		t.Errorf("el fallo del host debia loguearse como ERROR:\n%s", logs.String())
	}
	if count(t, `SELECT count(*) FROM metrics_chunks WHERE host_id = $1`, good) != 1 || count(t, `SELECT count(*) FROM metrics_chunks WHERE host_id = $1`, bad) != 0 {
		t.Error("el host bueno debia quedar empaquetado y el malo no")
	}

	mustExec(t, `DROP TRIGGER zz_fail ON metrics_chunks`)
	logs.reset()
	m.RunOnce(ctx)
	logs.noErrors(t)
	if hasPartition(t, tableMetrics, d) {
		t.Error("con todos los hosts empaquetados la particion debia borrarse")
	}
	if count(t, `SELECT count(*) FROM metrics_chunks`) != 2 {
		t.Error("ambos hosts debian tener su chunk")
	}
}

// El respaldo tras actualizar avanza packBudget particiones por ciclo, de la mas vieja a la mas nueva.
func TestPackBudgetLimitsWorkPerCycle(t *testing.T) {
	fresh(t)
	ctx := context.Background()
	now := farNow()
	today := utcDay(now)
	m, logs := newMaint(t, testCfg, now)
	h := mkHost(t, "srv-respaldo")
	d1, d2 := today.AddDate(0, 0, -12), today.AddDate(0, 0, -11)
	for _, d := range []time.Time{d1, d2} {
		mkPartition(t, tableMetrics, d)
		insertFull(t, testPool, h, fullRows(d.Add(time.Hour), 10))
	}

	m.RunOnce(ctx)
	logs.noErrors(t)
	if hasPartition(t, tableMetrics, d1) || !hasPartition(t, tableMetrics, d2) {
		t.Fatalf("ciclo 1: solo la mas vieja debia empaquetarse y borrarse (d1=%v d2=%v)", hasPartition(t, tableMetrics, d1), hasPartition(t, tableMetrics, d2))
	}
	m.RunOnce(ctx)
	logs.noErrors(t)
	if hasPartition(t, tableMetrics, d2) || count(t, `SELECT count(*) FROM metrics_chunks`) != 2 {
		t.Error("ciclo 2: la segunda particion debia empaquetarse y borrarse")
	}
}

// La particion legacy (la tabla original adoptada por la migracion) tambien se empaqueta antes de borrarse.
func TestPackLegacyPartition(t *testing.T) {
	fresh(t)
	ctx := context.Background()
	now := farNow()
	cfg := testCfg
	cfg.DetailDays = 730 // el dia de prueba queda ~105 dias atras: que el recorte de detalle no lo borre
	m, logs := newMaint(t, cfg, now)
	h := mkHost(t, "srv-legacy")
	d := utcDay(time.Now()).AddDate(0, 0, -5) // cae en metrics_legacy (MINVALUE .. hoy+2)
	rows := fullRows(d.Add(time.Hour), 15)
	insertFull(t, testPool, h, rows)
	if got := count(t, `SELECT count(*) FROM ONLY metrics_legacy`); got != len(rows) {
		t.Fatalf("filas en metrics_legacy = %d, want %d", got, len(rows))
	}
	// El resumen ya llego hasta el final: aqui se prueba el empaquetado, no la marca de agua.
	mustExec(t, `INSERT INTO maintenance_state (name, value_ts) VALUES ($1, $2)`, rollupMarker, now.Truncate(time.Hour))

	m.RunOnce(ctx)
	logs.noErrors(t)
	if hasLegacy(t, tableMetrics) {
		t.Fatal("metrics_legacy vencida y empaquetada debia borrarse")
	}
	sameRows(t, detail(t, h, d, d.AddDate(0, 0, 1)), rounded(rows))
}

// Una particion sin marca de agua (nada resumido) no se empaqueta ni se borra.
func TestNothingPackedWithoutRollupMark(t *testing.T) {
	fresh(t)
	now := farNow()
	today := utcDay(now)
	m, _ := newMaint(t, testCfg, now)
	h := mkHost(t, "srv-sin-marca")
	d := today.AddDate(0, 0, -12)
	mkPartition(t, tableMetrics, d)
	insertFull(t, testPool, h, fullRows(d.Add(time.Hour), 10))
	// Sin marca en maintenance_state el resumen la crea al empezar; un solo ciclo no llega a cubrir la particion.
	withConn(t, func(conn *pgx.Conn) {
		if err := m.dropExpired(context.Background(), conn, now); err != nil {
			t.Fatalf("dropExpired: %v", err)
		}
	})
	if !hasPartition(t, tableMetrics, d) || count(t, `SELECT count(*) FROM metrics_chunks`) != 0 {
		t.Error("sin marca de agua no debe empaquetarse ni borrarse nada")
	}
}
