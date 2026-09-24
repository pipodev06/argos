package retention

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestParseBound(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		want    [2]*time.Time
		wantOK  bool
		wantErr bool
	}{
		{name: "diaria", expr: `FOR VALUES FROM ('2026-09-25 00:00:00+00') TO ('2026-09-26 00:00:00+00')`,
			want: [2]*time.Time{tp(2026, 9, 25), tp(2026, 9, 26)}, wantOK: true},
		{name: "legacy sin cota inferior", expr: `FOR VALUES FROM (MINVALUE) TO ('2026-09-25 00:00:00+00')`,
			want: [2]*time.Time{nil, tp(2026, 9, 25)}, wantOK: true},
		{name: "sin cota superior", expr: `FOR VALUES FROM ('2026-09-25 00:00:00+00') TO (MAXVALUE)`,
			want: [2]*time.Time{tp(2026, 9, 25), nil}, wantOK: true},
		{name: "default", expr: `DEFAULT`},
		{name: "lista", expr: `FOR VALUES IN ('a')`},
		{name: "fecha rota", expr: `FOR VALUES FROM ('ayer') TO ('hoy')`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Maintainer{log: discardLogger()}
			p, ok := m.parsePartition("metrics", "metrics_x", false, tt.expr)
			if ok != tt.wantOK {
				t.Fatalf("ok got %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if !sameTime(p.lower, tt.want[0]) || !sameTime(p.upper, tt.want[1]) {
				t.Errorf("cotas got [%v, %v), want [%v, %v)", p.lower, p.upper, tt.want[0], tt.want[1])
			}
		})
	}
}

func TestCovers(t *testing.T) {
	d := func(day int) time.Time { return time.Date(2026, 9, day, 0, 0, 0, 0, time.UTC) }
	legacy := partition{name: "legacy", upper: tp(2026, 9, 25)}
	daily := partition{name: "d25", lower: tp(2026, 9, 25), upper: tp(2026, 9, 26)}
	parts := []partition{legacy, daily}

	tests := []struct {
		day  int
		want bool
	}{
		{24, true},  // dentro de la legacy (sin cota inferior)
		{25, true},  // particion diaria
		{26, false}, // libre
		{1, true},   // muy antiguo: tambien lo cubre la legacy
	}
	for _, tt := range tests {
		if got := covers(parts, d(tt.day), d(tt.day+1)); got != tt.want {
			t.Errorf("covers dia %d got %v, want %v", tt.day, got, tt.want)
		}
	}
}

func tp(y int, mo time.Month, d int) *time.Time {
	t := time.Date(y, mo, d, 0, 0, 0, 0, time.UTC)
	return &t
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

func TestCreatePartitionsIsIdempotentAndCorrect(t *testing.T) {
	fresh(t)
	ctx := context.Background()
	now := farNow()
	today := utcDay(now)
	m, logs := newMaint(t, testCfg, now)

	// Con la fecha real, metrics y service_checks (legacy + 7 diarias desde hoy+2) ya cubren hoy..hoy+7 y no hay
	// nada que crear; disk_samples (legacy hasta hoy + 7 diarias desde hoy) solo necesita el dia hoy+7.
	real, _ := newMaint(t, testCfg, time.Now())
	withConn(t, func(conn *pgx.Conn) {
		if err := real.createPartitions(ctx, conn, real.now()); err != nil {
			t.Fatal(err)
		}
	})
	base := map[string]int{tableMetrics: 8, tableServiceChecks: 8, tableDiskSamples: 9}
	for _, table := range partitionedTables {
		if got := len(partNames(t, table)); got != base[table] {
			t.Fatalf("%s: particiones tras crear con la fecha real got %d, want %d", table, got, base[table])
		}
	}

	create := func() {
		t.Helper()
		withConn(t, func(conn *pgx.Conn) {
			if err := m.createPartitions(ctx, conn, now); err != nil {
				t.Fatal(err)
			}
		})
	}
	create()
	for _, table := range partitionedTables {
		names := partNames(t, table)
		if got := len(names); got != base[table]+8 {
			t.Errorf("%s: particiones got %d, want %d", table, got, base[table]+8)
		}
		for i := 0; i <= createAheadDays; i++ {
			if !hasPartition(t, table, today.AddDate(0, 0, i)) {
				t.Errorf("%s: falta la particion de hoy+%d", table, i)
			}
		}
	}

	logs.reset()
	create() // segunda vez: nada nuevo, sin errores
	if s := logs.String(); s != "" {
		t.Errorf("la segunda ejecucion logueo algo: %s", s)
	}
	for _, table := range partitionedTables {
		if got := len(partNames(t, table)); got != base[table]+8 {
			t.Errorf("%s: particiones tras la segunda ejecucion got %d, want %d", table, got, base[table]+8)
		}
	}

	// Cada particion nueva quedo con PK/indice, FK y las cotas exactas (dia UTC).
	child := partitionName(tableMetrics, today)
	if got := count(t, `SELECT count(*) FROM pg_index WHERE indrelid = $1::regclass AND indisprimary`, child); got != 1 {
		t.Errorf("la particion %s no tiene PK (got %d)", child, got)
	}
	if got := count(t, `SELECT count(*) FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid JOIN pg_am a ON a.oid = c.relam
	                     WHERE i.indrelid = $1::regclass AND a.amname = 'brin'`, child); got != 1 {
		t.Errorf("la particion %s no tiene el indice BRIN de ts (got %d)", child, got)
	}
	if got := count(t, `SELECT count(*) FROM pg_constraint WHERE conrelid = $1::regclass AND contype = 'f'`, child); got != 1 {
		t.Errorf("la particion %s no tiene la FK a hosts (got %d)", child, got)
	}
	checks := partitionName(tableServiceChecks, today)
	if got := count(t, `SELECT count(*) FROM pg_constraint WHERE conrelid = $1::regclass AND contype = 'c' AND conname = 'service_checks_status_check'`, checks); got != 1 {
		t.Errorf("la particion %s no tiene el CHECK de status (got %d)", checks, got)
	}

	var parts []partition
	withConn(t, func(conn *pgx.Conn) {
		var err error
		if parts, err = m.listPartitions(ctx, conn, tableMetrics); err != nil {
			t.Fatal(err)
		}
	})
	for _, p := range parts {
		if p.name != partitionName(tableMetrics, today) {
			continue
		}
		if p.lower == nil || !p.lower.Equal(today) || p.upper == nil || !p.upper.Equal(today.AddDate(0, 0, 1)) {
			t.Errorf("cotas de %s got [%v, %v), want [%v, %v)", p.name, p.lower, p.upper, today, today.AddDate(0, 0, 1))
		}
	}

	// Una muestra dentro de la ventana entra; una mas alla de hoy+7 no tiene particion (23514 sin constraint).
	h := mkHost(t, "srv-part")
	mustExec(t, `INSERT INTO metrics (host_id, ts) VALUES ($1, $2)`, h, today.AddDate(0, 0, 3).Add(12*time.Hour))
	_, err := testPool.Exec(ctx, `INSERT INTO metrics (host_id, ts) VALUES ($1, $2)`, h, today.AddDate(0, 0, 9))
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != "" {
		t.Errorf("insert sin particion got %v, want SQLSTATE 23514 sin constraint", err)
	}
	logs.noErrors(t)
}

func TestCreatePartitionsRecreatesMissingDay(t *testing.T) {
	fresh(t)
	ctx := context.Background()
	now := farNow()
	today := utcDay(now)
	m, _ := newMaint(t, testCfg, now)

	withConn(t, func(conn *pgx.Conn) {
		if err := m.createPartitions(ctx, conn, now); err != nil {
			t.Fatal(err)
		}
	})
	// Se pierde el dia hoy+2 (p. ej. borrado a mano): el siguiente ciclo lo repone sin tocar el resto.
	mustExec(t, `DROP TABLE `+partitionName(tableMetrics, today.AddDate(0, 0, 2)))
	if hasPartition(t, tableMetrics, today.AddDate(0, 0, 2)) {
		t.Fatal("la particion sigue existiendo tras el DROP")
	}
	withConn(t, func(conn *pgx.Conn) {
		if err := m.createPartitions(ctx, conn, now); err != nil {
			t.Fatal(err)
		}
	})
	if !hasPartition(t, tableMetrics, today.AddDate(0, 0, 2)) {
		t.Errorf("no se repuso la particion de hoy+2")
	}
}

// Las cotas de las particiones son absolutas (offset +00): no dependen del timezone de la sesion
// del pool ni del que use pg_get_expr al listarlas.
func TestPartitionsAreIndependentOfSessionTimezone(t *testing.T) {
	fresh(t)
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(testDSN)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["timezone"] = "Asia/Tokyo"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	now := farNow()
	today := utcDay(now)
	m := New(pool, discardLogger(), testCfg, func() time.Time { return now })
	pc, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Release()
	var tz string
	if err := pc.Conn().QueryRow(ctx, `SHOW timezone`).Scan(&tz); err != nil || tz != "Asia/Tokyo" {
		t.Fatalf("timezone de la sesion got %q (%v), want Asia/Tokyo", tz, err)
	}

	if err := m.createPartitions(ctx, pc.Conn(), now); err != nil {
		t.Fatal(err)
	}
	parts, err := m.listPartitions(ctx, pc.Conn(), tableMetrics)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, p := range parts {
		for i := 0; i <= createAheadDays; i++ {
			day := today.AddDate(0, 0, i)
			if p.name == partitionName(tableMetrics, day) {
				found++
				if p.lower == nil || !p.lower.Equal(day) || p.upper == nil || !p.upper.Equal(day.AddDate(0, 0, 1)) {
					t.Errorf("cotas de %s got [%v, %v), want [%v, %v) (medianoche UTC)", p.name, p.lower, p.upper, day, day.AddDate(0, 0, 1))
				}
			}
		}
	}
	if found != createAheadDays+1 {
		t.Errorf("particiones diarias encontradas got %d, want %d", found, createAheadDays+1)
	}
}
