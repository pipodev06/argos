package store

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestMigrationSNMPConstraintsAndDown comprueba los CHECK de la migracion 24 (forma del perfil, perfil solo en
// dispositivos, forma del chequeo snmp) y que Down borra lo de SNMP sin tocar lo demas. Deja la BD en el ultimo esquema.
func TestMigrationSNMPConstraintsAndDown(t *testing.T) {
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

	const prof = `00000000-0000-4000-8000-0000000000a1`
	mustExec(t, `INSERT INTO snmp_profiles (id, name, version, username, auth_proto, priv_proto, secret_enc, secret_key_id)
	             VALUES ($1, 'p3', 3, 'u', 'SHA256', 'AES128', '\x01', 'k')`, prof)
	mustExec(t, `INSERT INTO hosts (hostname, agent_token_hash) VALUES ('col', $1)`, th("col"))
	mustExec(t, `INSERT INTO hosts (hostname, kind, address, collector_host_id, snmp_profile_id)
	             SELECT 'dev', 'device', '10.9.9.9', id, $1 FROM hosts WHERE hostname = 'col'`, prof)
	mustExec(t, `INSERT INTO service_check_defs (name, type, snmp_metric, snmp_if_index) VALUES ('snmp-if', 'snmp', 'if_oper', 2)`)

	for name, q := range map[string]string{
		"v3 sin usuario":       `INSERT INTO snmp_profiles (id, name, version, auth_proto, priv_proto, secret_enc, secret_key_id) VALUES (gen_random_uuid(), 'x', 3, 'SHA256', 'AES128', '\x01', 'k')`,
		"v2 con usuario":       `INSERT INTO snmp_profiles (id, name, version, username, secret_enc, secret_key_id) VALUES (gen_random_uuid(), 'x', 2, 'u', '\x01', 'k')`,
		"version 1":            `INSERT INTO snmp_profiles (id, name, version, secret_enc, secret_key_id) VALUES (gen_random_uuid(), 'x', 1, '\x01', 'k')`,
		"MD5":                  `INSERT INTO snmp_profiles (id, name, version, username, auth_proto, priv_proto, secret_enc, secret_key_id) VALUES (gen_random_uuid(), 'x', 3, 'u', 'MD5', 'AES128', '\x01', 'k')`,
		"DES":                  `INSERT INTO snmp_profiles (id, name, version, username, auth_proto, priv_proto, secret_enc, secret_key_id) VALUES (gen_random_uuid(), 'x', 3, 'u', 'SHA256', 'DES', '\x01', 'k')`,
		"nombre duplicado":     `INSERT INTO snmp_profiles (id, name, version, username, auth_proto, priv_proto, secret_enc, secret_key_id) VALUES (gen_random_uuid(), 'P3', 3, 'u', 'SHA256', 'AES128', '\x01', 'k')`,
		"perfil en un agente":  `UPDATE hosts SET snmp_profile_id = '` + prof + `' WHERE hostname = 'col'`,
		"snmp sin metrica":     `INSERT INTO service_check_defs (name, type) VALUES ('x', 'snmp')`,
		"reachable con indice": `INSERT INTO service_check_defs (name, type, snmp_metric, snmp_if_index) VALUES ('x', 'snmp', 'reachable', 1)`,
		"if_oper sin indice":   `INSERT INTO service_check_defs (name, type, snmp_metric) VALUES ('x', 'snmp', 'if_oper')`,
		"indice fuera":         `INSERT INTO service_check_defs (name, type, snmp_metric, snmp_if_index) VALUES ('x', 'snmp', 'if_oper', 70000)`,
		"snmp con umbral":      `INSERT INTO service_check_defs (name, type, snmp_metric, warn_ms) VALUES ('x', 'snmp', 'reachable', 5)`,
		"tcp con metrica":      `INSERT INTO service_check_defs (name, type, target_host, target_port, snmp_metric) VALUES ('x', 'tcp', 'h', 1, 'reachable')`,
		"borrar perfil en uso": `DELETE FROM snmp_profiles WHERE id = '` + prof + `'`,
	} {
		if _, err := testPool.Exec(ctx, q); err == nil {
			t.Errorf("%s debe rechazarse", name)
		}
	}

	if _, err := provider.DownTo(ctx, 23); err != nil {
		t.Fatalf("down a 23: %v", err)
	}
	if n := count(t, `SELECT count(*) FROM service_check_defs WHERE type = 'snmp'`); n != 0 {
		t.Errorf("Down debe borrar los chequeos snmp, quedan %d", n)
	}
	if n := count(t, `SELECT count(*) FROM hosts WHERE hostname IN ('col', 'dev')`); n != 2 {
		t.Errorf("Down no debe borrar hosts ni dispositivos, quedan %d de 2", n)
	}
	if n := count(t, `SELECT count(*) FROM pg_tables WHERE schemaname = 'public' AND tablename = 'snmp_profiles'`); n != 0 {
		t.Error("snmp_profiles debe haberse eliminado")
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("up a 24: %v", err)
	}
}
