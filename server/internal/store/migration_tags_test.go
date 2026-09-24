package store

import (
	"context"
	"testing"
)

// TestMigrationHostTagsDataSafety baja a la migracion 15 (etiquetas clave=valor), siembra reglas y ventanas con
// alcance de etiqueta y sube a la 16: las reglas quedan DESACTIVADAS (nunca activas y globales), las ventanas se
// eliminan, las de grupo/all se conservan y la tabla nueva queda vacia. Deja la BD en el ultimo esquema.
func TestMigrationHostTagsDataSafety(t *testing.T) {
	newTestStore(t)
	ctx := context.Background()
	provider, err := newProvider(testPool)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := provider.Up(ctx); err != nil {
			t.Errorf("restaurando esquema: %v", err)
		}
	})
	if _, err := provider.DownTo(ctx, 15); err != nil {
		t.Fatalf("down a 15: %v", err)
	}

	mustExec(t, `INSERT INTO hosts (hostname, agent_token_hash) VALUES ('h', $1)`, th("h"))
	mustExec(t, `INSERT INTO host_labels (host_id, key, value) SELECT id, 'env', 'prod' FROM hosts`)
	mustExec(t, `INSERT INTO alert_rules (name, kind, duration_seconds, severity, scope_type, scope_label_key, scope_label_value)
	             VALUES ('por etiqueta', 'host_status', 0, 'info', 'label', 'env', 'prod')`)
	mustExec(t, `INSERT INTO alert_rules (name, kind, duration_seconds, severity) VALUES ('global', 'host_status', 0, 'info')`)
	mustExec(t, `INSERT INTO maintenance_windows (name, starts_at, ends_at, scope_type, scope_label_key, scope_label_value)
	             VALUES ('vent etiqueta', now(), now() + interval '1 hour', 'label', 'env', 'prod')`)
	mustExec(t, `INSERT INTO maintenance_windows (name, starts_at, ends_at, scope_type)
	             VALUES ('vent global', now(), now() + interval '1 hour', 'all')`)

	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("up a 16: %v", err)
	}
	if n := count(t, `SELECT count(*) FROM alert_rules WHERE name = 'por etiqueta' AND NOT enabled AND scope_type = 'all'`); n != 1 {
		t.Errorf("la regla con etiqueta debe quedar desactivada, got %d", n)
	}
	if n := count(t, `SELECT count(*) FROM alert_rules WHERE name = 'global' AND enabled`); n != 1 {
		t.Errorf("la regla global debe seguir activa, got %d", n)
	}
	if n := count(t, `SELECT count(*) FROM maintenance_windows`); n != 1 {
		t.Errorf("ventanas tras migrar got %d, want 1 (solo la global)", n)
	}
	if n := count(t, `SELECT count(*) FROM host_tags`); n != 0 {
		t.Errorf("host_tags debe empezar vacia, got %d", n)
	}
	// Las restricciones nuevas rechazan un alcance incoherente.
	if _, err := testPool.Exec(ctx, `INSERT INTO alert_rules (name, kind, duration_seconds, severity, scope_type)
	                                 VALUES ('x', 'host_status', 0, 'info', 'tag')`); err == nil {
		t.Error("scope tag sin scope_tag debe rechazarse")
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO alert_rules (name, kind, duration_seconds, severity, scope_type, scope_tag)
	                                 VALUES ('x', 'host_status', 0, 'info', 'all', 'dev')`); err == nil {
		t.Error("scope all con scope_tag debe rechazarse")
	}
}
