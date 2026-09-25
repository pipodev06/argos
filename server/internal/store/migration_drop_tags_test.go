package store

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestMigrationDropHostTagsDataSafety baja a la migracion 17 (con etiquetas), siembra reglas, chequeos y
// ventanas con alcance de etiqueta y sube a la 18: reglas y chequeos quedan DESACTIVADOS con alcance all
// (nunca activos y globales), las ventanas se eliminan, lo demas se conserva y el NOTICE lleva los conteos.
// Luego verifica Down (estructura vacia) y Up de nuevo. Deja la BD en el ultimo esquema.
func TestMigrationDropHostTagsDataSafety(t *testing.T) {
	newTestStore(t)
	ctx := context.Background()

	// Pool propio (1 conexion) pa capturar los NOTICE de la migracion.
	cfg, err := pgxpool.ParseConfig(os.Getenv("ARGOS_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 1
	var mu sync.Mutex
	var notices []string
	cfg.ConnConfig.OnNotice = func(_ *pgconn.PgConn, n *pgconn.Notice) {
		mu.Lock()
		defer mu.Unlock()
		notices = append(notices, n.Message)
	}
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
	if _, err := provider.DownTo(ctx, 17); err != nil {
		t.Fatalf("down a 17: %v", err)
	}

	mustExec(t, `INSERT INTO hosts (hostname, agent_token_hash) VALUES ('h', $1)`, th("h"))
	mustExec(t, `INSERT INTO host_tags (host_id, tag) SELECT id, 'prod' FROM hosts`)
	mustExec(t, `INSERT INTO alert_rules (name, kind, duration_seconds, severity, scope_type, scope_tag)
	             VALUES ('regla etiqueta', 'host_status', 0, 'info', 'tag', 'prod')`)
	mustExec(t, `INSERT INTO alert_rules (name, kind, duration_seconds, severity) VALUES ('regla global', 'host_status', 0, 'info')`)
	mustExec(t, `INSERT INTO service_check_defs (name, type, target_host, target_port, scope_type, scope_tag)
	             VALUES ('chequeo etiqueta', 'tcp', 'localhost', 22, 'tag', 'prod')`)
	mustExec(t, `INSERT INTO service_check_defs (name, type, target_host, target_port) VALUES ('chequeo global', 'tcp', 'localhost', 22)`)
	mustExec(t, `INSERT INTO maintenance_windows (name, starts_at, ends_at, scope_type, scope_tag)
	             VALUES ('vent etiqueta', now(), now() + interval '1 hour', 'tag', 'prod')`)
	mustExec(t, `INSERT INTO maintenance_windows (name, starts_at, ends_at, scope_type)
	             VALUES ('vent global', now(), now() + interval '1 hour', 'all')`)

	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("up a 18: %v", err)
	}
	mu.Lock()
	got := strings.Join(notices, "|")
	mu.Unlock()
	if !strings.Contains(got, "1 reglas desactivadas, 1 chequeos desactivados, 1 ventanas eliminadas") {
		t.Errorf("NOTICE got %q, want los conteos 1/1/1", got)
	}
	if n := count(t, `SELECT count(*) FROM alert_rules WHERE name = 'regla etiqueta' AND NOT enabled AND scope_type = 'all'`); n != 1 {
		t.Errorf("la regla con etiqueta debe quedar desactivada con alcance all, got %d", n)
	}
	if n := count(t, `SELECT count(*) FROM alert_rules WHERE name = 'regla global' AND enabled`); n != 1 {
		t.Errorf("la regla global debe seguir activa, got %d", n)
	}
	if n := count(t, `SELECT count(*) FROM service_check_defs WHERE name = 'chequeo etiqueta' AND NOT enabled AND scope_type = 'all'`); n != 1 {
		t.Errorf("el chequeo con etiqueta debe quedar desactivado con alcance all, got %d", n)
	}
	if n := count(t, `SELECT count(*) FROM service_check_defs WHERE name = 'chequeo global' AND enabled`); n != 1 {
		t.Errorf("el chequeo global debe seguir activo, got %d", n)
	}
	if n := count(t, `SELECT count(*) FROM maintenance_windows`); n != 1 {
		t.Errorf("ventanas tras migrar got %d, want 1 (solo la global)", n)
	}
	if n := count(t, `SELECT count(*) FROM pg_tables WHERE schemaname = 'public' AND tablename = 'host_tags'`); n != 0 {
		t.Error("host_tags debe haberse eliminado")
	}
	if n := count(t, `SELECT count(*) FROM information_schema.columns WHERE table_schema = 'public' AND column_name = 'scope_tag'`); n != 0 {
		t.Errorf("scope_tag debe haberse eliminado, quedan %d columnas", n)
	}
	// Las restricciones nuevas rechazan el alcance 'tag' y los incoherentes.
	for name, q := range map[string]string{
		"regla tag":     `INSERT INTO alert_rules (name, kind, duration_seconds, severity, scope_type) VALUES ('x', 'host_status', 0, 'info', 'tag')`,
		"regla group":   `INSERT INTO alert_rules (name, kind, duration_seconds, severity, scope_type) VALUES ('x', 'host_status', 0, 'info', 'group')`,
		"ventana tag":   `INSERT INTO maintenance_windows (name, starts_at, ends_at, scope_type) VALUES ('x', now(), now() + interval '1 hour', 'tag')`,
		"chequeo tag":   `INSERT INTO service_check_defs (name, type, target_host, target_port, scope_type) VALUES ('x', 'tcp', 'h', 1, 'tag')`,
		"chequeo group": `INSERT INTO service_check_defs (name, type, target_host, target_port, scope_type) VALUES ('x', 'tcp', 'h', 1, 'group')`,
	} {
		if _, err := testPool.Exec(ctx, q); err == nil {
			t.Errorf("%s debe rechazarse", name)
		}
	}
	// La vista de evaluacion sigue resolviendo alcance all/group.
	if _, err := testPool.Exec(ctx, `SELECT * FROM alert_candidates`); err != nil {
		t.Errorf("alert_candidates: %v", err)
	}

	// Down recrea la estructura de etiquetas vacia; Up la vuelve a quitar.
	if _, err := provider.DownTo(ctx, 17); err != nil {
		t.Fatalf("down a 17 (2): %v", err)
	}
	if n := count(t, `SELECT count(*) FROM host_tags`); n != 0 {
		t.Errorf("host_tags tras Down got %d filas, want 0", n)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("up a 18 (2): %v", err)
	}
}
