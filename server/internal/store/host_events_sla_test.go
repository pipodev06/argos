package store

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// transitions devuelve el historial de un host como "from>to:source" en orden cronologico (from vacio = evento inicial).
func transitions(t *testing.T, id string) []string {
	t.Helper()
	rows, err := testPool.Query(context.Background(),
		`SELECT COALESCE(from_status, '') || '>' || to_status || ':' || source FROM host_events WHERE host_id = $1 ORDER BY at, id`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestHostEventsRecordEveryPath(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	admin := mkUser(t, s, "admin", "admin")
	if err := mkEnrollToken(ctx, s, th("tok"), "srv", admin, nil, time.Now().Add(time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	id, _, err := enrollHost(ctx, s, th("tok"), "linux", "amd64", th("agent"))
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, `UPDATE hosts SET last_seen_at = now() - interval '1 hour' WHERE id = $1`, id)
	if _, err := s.MarkStaleHosts(ctx, 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkStaleHosts(ctx, 2*time.Minute); err != nil { // sin cambio: no emite
		t.Fatal(err)
	}
	if _, err := s.IngestMetrics(ctx, id, Sample{Timestamp: time.Now()}); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if err := s.RevokeHost(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeHost(ctx, id); err != nil { // idempotente: no emite
		t.Fatal(err)
	}
	want := []string{">up:enroll", "up>offline:detector", "offline>up:ingest", "up>disabled:admin"}
	if got := transitions(t, id); !slices.Equal(got, want) {
		t.Errorf("transiciones got %v, want %v", got, want)
	}
}

func TestPurgeHostEventsKeepsLastPerHost(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedHost(t, "a", "up", ago(time.Second))
	seedHost(t, "b", "up", ago(time.Second))
	a, b := hostID(t, "a"), hostID(t, "b")
	mustExec(t, `DELETE FROM host_events`)
	mustExec(t, `INSERT INTO host_events (host_id, from_status, to_status, at, source) VALUES
		($1, NULL, 'up', now() - interval '500 days', 'seed'),
		($1, 'up', 'down', now() - interval '450 days', 'detector'),
		($1, 'down', 'up', now() - interval '420 days', 'ingest'),
		($1, 'up', 'down', now() - interval '10 days', 'detector'),
		($2, NULL, 'up', now() - interval '600 days', 'seed')`, a, b)
	cut := time.Now().Add(-400 * 24 * time.Hour)
	if n, err := s.PurgeHostEvents(ctx, cut, 1000); err != nil || n != 2 {
		t.Fatalf("purga got %d, %v; want 2", n, err)
	}
	if got, want := transitions(t, a), []string{"down>up:ingest", "up>down:detector"}; !slices.Equal(got, want) {
		t.Errorf("host a got %v, want %v", got, want)
	}
	if got := transitions(t, b); len(got) != 1 {
		t.Errorf("host b debe conservar su unico evento, got %v", got)
	}
	if n, err := s.PurgeHostEvents(ctx, cut, 1000); err != nil || n != 0 {
		t.Errorf("segunda purga got %d, %v; want 0", n, err)
	}
}

func TestHostEventsInRangeScope(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	gA, gB := mkGroup(t, s, "A", true, nil), mkGroup(t, s, "B", true, nil)
	for _, h := range []string{"ha", "hb", "hn"} {
		seedHost(t, h, "up", ago(time.Second))
	}
	setGroupOf(t, "ha", gA.ID)
	setGroupOf(t, "hb", gB.ID)
	mustExec(t, `DELETE FROM host_events`)
	for _, h := range []string{"ha", "hb", "hn"} {
		mustExec(t, `INSERT INTO host_events (host_id, from_status, to_status, at, source) VALUES
			($1, NULL, 'up', now() - interval '20 days', 'seed'),
			($1, 'up', 'down', now() - interval '5 days', 'detector'),
			($1, 'down', 'up', now() - interval '1 hour', 'ingest')`, hostID(t, h))
	}
	from, to := time.Now().Add(-10*24*time.Hour), time.Now()

	for _, tc := range []struct {
		name  string
		scope Scope
		hosts int
		rows  int
	}{
		{"total", AllScope(), 3, 9},
		{"grupo A", GroupScope([]string{gA.ID}), 1, 3},
		{"sin grupos", GroupScope(nil), 0, 0},
		{"valor cero", Scope{}, 0, 0},
	} {
		got, err := s.HostEventsInRange(ctx, tc.scope, from, to)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		seen := map[string]bool{}
		for _, e := range got {
			seen[e.HostID] = true
		}
		if len(got) != tc.rows || len(seen) != tc.hosts {
			t.Errorf("%s: got %d filas / %d hosts, want %d / %d", tc.name, len(got), len(seen), tc.rows, tc.hosts)
		}
	}
	// El evento previo a from viene primero; los del rango, despues y en orden.
	got, err := s.HostEventsInRange(ctx, GroupScope([]string{gA.ID}), from, to)
	if err != nil || len(got) != 3 {
		t.Fatalf("rango A: %v, %d filas", err, len(got))
	}
	if got[0].Source != "seed" || got[0].FromStatus != nil || got[1].ToStatus != "down" || got[2].ToStatus != "up" {
		t.Errorf("orden/contenido inesperado: %+v", got)
	}
}

func TestMigrationHostEventsSeedUpDown(t *testing.T) {
	newTestStore(t)
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(os.Getenv("ARGOS_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	provider, err := newProvider(pool)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := provider.Up(ctx); err != nil {
			t.Errorf("restaurando esquema: %v", err)
		}
	})
	if _, err := provider.DownTo(ctx, 19); err != nil {
		t.Fatalf("down a 19: %v", err)
	}
	mustExec(t, `INSERT INTO hosts (hostname, agent_token_hash, status, status_changed_at) VALUES ('viejo', $1, 'down', now() - interval '3 days')`, th("v"))
	mustExec(t, `INSERT INTO hosts (hostname, agent_token_hash, status) VALUES ('nuevo', $1, 'pending')`, th("s"))
	mustExec(t, `INSERT INTO hosts (hostname, agent_token_hash, status) VALUES ('con-historial', $1, 'up')`, th("c"))
	mustExec(t, `INSERT INTO host_events (host_id, from_status, to_status, source)
		SELECT id, 'down', 'up', 'ingest' FROM hosts WHERE hostname = 'con-historial'`)

	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("up a 20: %v", err)
	}
	if got, want := transitions(t, hostID(t, "viejo")), []string{">down:seed"}; !slices.Equal(got, want) {
		t.Errorf("viejo got %v, want %v", got, want)
	}
	if n := count(t, `SELECT count(*) FROM host_events e JOIN hosts h ON h.id = e.host_id
		WHERE h.hostname = 'viejo' AND e.at < now() - interval '2 days'`); n != 1 {
		t.Errorf("la siembra debe usar status_changed_at, got %d", n)
	}
	if got, want := transitions(t, hostID(t, "nuevo")), []string{">pending:seed"}; !slices.Equal(got, want) {
		t.Errorf("nuevo got %v, want %v", got, want)
	}
	if got := transitions(t, hostID(t, "con-historial")); len(got) != 1 || got[0] != "down>up:ingest" {
		t.Errorf("con historial no se siembra, got %v", got)
	}

	if _, err := provider.DownTo(ctx, 19); err != nil {
		t.Fatalf("down a 19 (2): %v", err)
	}
	if n := count(t, `SELECT count(*) FROM host_events`); n != 1 {
		t.Errorf("Down debe quitar las siembras y dejar 1 evento, got %d", n)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO host_events (host_id, from_status, to_status, source)
		SELECT id, NULL, 'up', 'seed' FROM hosts LIMIT 1`); err == nil {
		t.Error("tras Down un from_status NULL debe rechazarse")
	}
}

// Carga sintetica (100 hosts x 40 transiciones = 4000 filas): la lectura de un rango la devuelve completa y el
// tamano por fila se registra con t.Logf (go test -v).
func TestHostEventsLoadVolume(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mustExec(t, `INSERT INTO hosts (hostname, agent_token_hash, status)
		SELECT 'h' || g, 'hash-' || g, 'up' FROM generate_series(1, 100) g`)
	mustExec(t, `DELETE FROM host_events`)
	mustExec(t, `INSERT INTO host_events (host_id, from_status, to_status, at, source)
		SELECT h.id, CASE WHEN n % 2 = 1 THEN 'up' ELSE 'down' END, CASE WHEN n % 2 = 1 THEN 'down' ELSE 'up' END,
		       now() - make_interval(hours => 40 - n), 'detector'
		  FROM hosts h CROSS JOIN generate_series(1, 40) n`)
	start := time.Now()
	got, err := s.HostEventsInRange(ctx, AllScope(), time.Now().Add(-90*24*time.Hour), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if len(got) != 4000 {
		t.Errorf("filas got %d, want 4000", len(got))
	}
	var size int64
	if err := testPool.QueryRow(ctx, `SELECT pg_total_relation_size('host_events')`).Scan(&size); err != nil {
		t.Fatal(err)
	}
	t.Logf("carga: 4000 filas, %d bytes en total con indices (~%d B/fila), lectura de rango %v", size, size/4000, elapsed)
}
