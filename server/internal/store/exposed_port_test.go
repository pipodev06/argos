package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Tests de la alerta exposed_port (puerto nuevo expuesto) contra Postgres real.

// ingestPorts envia una foto del modulo puertos (items distintos generan un rev distinto).
func ingestPorts(t *testing.T, s *Store, host string, items ...string) {
	t.Helper()
	raw := make([]json.RawMessage, len(items))
	for i, it := range items {
		raw[i] = json.RawMessage(it)
	}
	sum := sha256.Sum256([]byte(strings.Join(items, ",")))
	in := InventoryFull{Module: InventoryPorts, Rev: hex.EncodeToString(sum[:]), Items: raw, CollectedAt: time.Now()}
	if err := s.IngestInventory(context.Background(), hostID(t, host), in, time.Now()); err != nil {
		t.Fatalf("ingest de puertos: %v", err)
	}
}

func portItem(proto, bind string, port int) string {
	return fmt.Sprintf(`{"proto":%q,"bind":%q,"port":%d}`, proto, bind, port)
}

func exposedRule(name string) AlertRule {
	return AlertRule{Name: name, Kind: "exposed_port", ResolveSeconds: 0, Scope: AlertScope{Type: ScopeAll}, Severity: "warn", Enabled: true}
}

func alertDetail(t *testing.T) string {
	t.Helper()
	var d *string
	if err := testPool.QueryRow(context.Background(), `SELECT detail FROM alerts`).Scan(&d); err != nil {
		t.Fatal(err)
	}
	if d == nil {
		return ""
	}
	return *d
}

func TestExposedPortBaselineDoesNotAlertButNewPortDoes(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "srv", "up", ago(time.Second))
	mkRule(t, s, exposedRule("puertos"))

	ingestPorts(t, s, "srv", portItem("tcp", "0.0.0.0", 22)) // linea base
	if r := evalOnce(t, s); r.Opened != 0 {
		t.Fatalf("la linea base no debe alertar, opened got %d", r.Opened)
	}
	// Nuevo expuesto (IPv4 comodin) + nuevo en loopback (v4 y v6): solo el primero cuenta.
	ingestPorts(t, s, "srv", portItem("tcp", "0.0.0.0", 22), portItem("tcp", "0.0.0.0", 8080),
		portItem("tcp", "127.0.0.1", 5432), portItem("tcp", "::1", 6379))
	if r := evalOnce(t, s); r.Opened != 1 {
		t.Fatalf("opened got %d, want 1", r.Opened)
	}
	a := oneAlert(t)
	if a.Trigger == nil || *a.Trigger != 1 {
		t.Errorf("trigger got %v, want 1", a.Trigger)
	}
	if got := alertDetail(t); got != "tcp/8080" {
		t.Errorf("detail got %q, want tcp/8080", got)
	}
	// Otro puerto nuevo (bind vacio = expuesto): la misma alerta se actualiza, no se duplica.
	ingestPorts(t, s, "srv", portItem("tcp", "0.0.0.0", 22), portItem("tcp", "0.0.0.0", 8080), portItem("udp", "", 53))
	if r := evalOnce(t, s); r.Opened != 0 {
		t.Errorf("no debe duplicar: %+v", r)
	}
	if got := alertDetail(t); got != "udp/53, tcp/8080" {
		t.Errorf("detail got %q, want udp/53, tcp/8080", got)
	}
	if a := oneAlert(t); a.Peak == nil || *a.Peak != 2 {
		t.Errorf("pico got %v, want 2", a.Peak)
	}
	// Desaparecen: se resuelve.
	ingestPorts(t, s, "srv", portItem("tcp", "0.0.0.0", 22))
	evalOnce(t, s)
	evalOnce(t, s)
	if a := oneAlert(t); a.State != "resolved" || a.Reason != "recovered" {
		t.Errorf("alerta got %+v, want resolved/recovered", a)
	}
}

func TestExposedPortLoopbackOnlyNeverAlerts(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "srv", "up", ago(time.Second))
	mkRule(t, s, exposedRule("puertos"))
	ingestPorts(t, s, "srv")
	ingestPorts(t, s, "srv", portItem("tcp", "127.0.0.1", 5432), portItem("tcp", "127.0.1.1", 53),
		portItem("tcp", "::1", 631), portItem("tcp", "::ffff:127.0.0.1", 9000))
	if r := evalOnce(t, s); r.Opened != 0 {
		t.Fatalf("loopback no debe alertar, opened got %d", r.Opened)
	}
}

func TestExposedPortExpectedPortsAreIgnored(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "srv", "up", ago(time.Second))
	rule := exposedRule("puertos")
	rule.ExpectedPorts = []ExpectedPort{{Proto: "tcp", Port: 8080}}
	mkRule(t, s, rule)
	ingestPorts(t, s, "srv")
	ingestPorts(t, s, "srv", portItem("tcp", "0.0.0.0", 8080))
	if r := evalOnce(t, s); r.Opened != 0 {
		t.Fatalf("puerto esperado no debe alertar, opened got %d", r.Opened)
	}
	// El mismo numero en otro protocolo no esta esperado.
	ingestPorts(t, s, "srv", portItem("tcp", "0.0.0.0", 8080), portItem("udp", "0.0.0.0", 8080))
	if r := evalOnce(t, s); r.Opened != 1 {
		t.Fatalf("udp/8080 no esta esperado, opened got %d, want 1", r.Opened)
	}
	if got := alertDetail(t); got != "udp/8080" {
		t.Errorf("detail got %q", got)
	}
}

func TestExposedPortDetailListsAtMostFive(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "srv", "up", ago(time.Second))
	mkRule(t, s, exposedRule("puertos"))
	ingestPorts(t, s, "srv")
	var items []string
	for p := 9001; p <= 9007; p++ {
		items = append(items, portItem("tcp", "0.0.0.0", p), portItem("tcp", "::", p)) // v4 y v6 cuentan una vez
	}
	ingestPorts(t, s, "srv", items...)
	evalOnce(t, s)
	if a := oneAlert(t); a.Trigger == nil || *a.Trigger != 7 {
		t.Errorf("trigger got %v, want 7", a.Trigger)
	}
	if got := alertDetail(t); got != "tcp/9001, tcp/9002, tcp/9003, tcp/9004, tcp/9005" {
		t.Errorf("detail got %q", got)
	}
}

func TestExposedPortMaintenanceSuppressesNotification(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "srv", "up", ago(time.Second))
	rule := mkRule(t, s, exposedRule("puertos"))
	mkChannel(t, "hook", "info", rule.ID)
	mustExec(t, `INSERT INTO maintenance_windows (name, starts_at, ends_at, scope_type)
		VALUES ('parche', now() - interval '1 hour', now() + interval '1 hour', 'all')`)
	ingestPorts(t, s, "srv")
	ingestPorts(t, s, "srv", portItem("tcp", "0.0.0.0", 4444))
	evalOnce(t, s)
	if a := oneAlert(t); !a.Suppressed {
		t.Error("en mantenimiento la alerta debe quedar suppressed")
	}
	if deliveries(t, "opened", "suppressed") != 1 || deliveries(t, "opened", "pending") != 0 {
		t.Error("la entrega opened debe quedar suppressed")
	}
}

func TestExposedPortRespectsGroupScope(t *testing.T) {
	s := newTestStore(t)
	g := mkGroup(t, s, "prod", false, nil)
	for _, n := range []string{"a", "b"} {
		seedHost(t, n, "up", ago(time.Second))
		ingestPorts(t, s, n)
		ingestPorts(t, s, n, portItem("tcp", "0.0.0.0", 4444))
	}
	setGroupOf(t, "a", g.ID)
	rule := exposedRule("solo prod")
	rule.Scope = AlertScope{Type: ScopeGroup, GroupID: &g.ID}
	mkRule(t, s, rule)
	if r := evalOnce(t, s); r.Opened != 1 {
		t.Fatalf("opened got %d, want 1", r.Opened)
	}
	if n := count(t, `SELECT count(*) FROM alerts a JOIN hosts h ON h.id = a.host_id WHERE h.hostname <> 'a'`); n != 0 {
		t.Error("el host fuera del grupo no debe alertar")
	}
}

func TestExposedPortRuleRoundTripAndConstraints(t *testing.T) {
	s := newTestStore(t)
	rule := exposedRule("puertos")
	rule.ExpectedPorts = []ExpectedPort{{Proto: "tcp", Port: 443}, {Proto: "udp", Port: 53}}
	got := mkRule(t, s, rule)
	if len(got.ExpectedPorts) != 2 || got.ExpectedPorts[1] != (ExpectedPort{Proto: "udp", Port: 53}) {
		t.Fatalf("expected_ports got %+v", got.ExpectedPorts)
	}
	upd, err := s.UpdateAlertRule(context.Background(), got.ID, func(r *AlertRule) error {
		r.ExpectedPorts = nil
		return nil
	})
	if err != nil || len(upd.ExpectedPorts) != 0 {
		t.Fatalf("update got %+v, %v", upd.ExpectedPorts, err)
	}
	// Los CHECK de la BD son la ultima defensa.
	for name, sql := range map[string]string{
		"proto invalido":     `UPDATE alert_rules SET expected_ports = '[{"proto":"icmp","port":1}]' WHERE id = $1`,
		"puerto fuera":       `UPDATE alert_rules SET expected_ports = '[{"proto":"tcp","port":70000}]' WHERE id = $1`,
		"no es arreglo":      `UPDATE alert_rules SET expected_ports = '{}' WHERE id = $1`,
		"elemento no objeto": `UPDATE alert_rules SET expected_ports = '[5]' WHERE id = $1`,
		"kind con umbral":    `UPDATE alert_rules SET threshold = 1 WHERE id = $1`,
	} {
		if _, err := testPool.Exec(context.Background(), sql, got.ID); err == nil {
			t.Errorf("%s: la BD debio rechazarlo", name)
		}
	}
	over := make([]string, 101)
	for i := range over {
		over[i] = fmt.Sprintf(`{"proto":"tcp","port":%d}`, i+1)
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE alert_rules SET expected_ports = $2::jsonb WHERE id = $1`,
		got.ID, "["+strings.Join(over, ",")+"]"); err == nil {
		t.Error("101 puertos esperados: la BD debio rechazarlo")
	}
	// expected_ports solo en reglas exposed_port.
	mustExec(t, `INSERT INTO alert_rules (name, kind, threshold, duration_seconds, resolve_seconds, severity, scope_type)
		VALUES ('nd', 'no_data', 300, 0, 0, 'warn', 'all')`)
	if _, err := testPool.Exec(context.Background(), `UPDATE alert_rules SET expected_ports = '[{"proto":"tcp","port":1}]' WHERE name = 'nd'`); err == nil {
		t.Error("expected_ports en una regla no_data: la BD debio rechazarlo")
	}
}

func TestMigrateDownExposedPortAndUpAgain(t *testing.T) {
	if testPool == nil {
		t.Skip("ARGOS_TEST_DATABASE_URL no definida")
	}
	ctx := context.Background()
	if err := resetSchema(ctx); err != nil {
		t.Fatal(err)
	}
	mustExec(t, `INSERT INTO alert_rules (name, kind, duration_seconds, resolve_seconds, severity, scope_type, expected_ports)
		VALUES ('p', 'exposed_port', 0, 0, 'warn', 'all', '[{"proto":"tcp","port":1}]')`)
	provider, err := newProvider(testPool)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.DownTo(ctx, 21); err != nil {
		t.Fatalf("down a 21: %v", err)
	}
	if got := count(t, `SELECT count(*) FROM information_schema.columns WHERE table_name = 'alert_rules' AND column_name = 'expected_ports'`); got != 0 {
		t.Error("expected_ports sigue tras el down")
	}
	if got := count(t, `SELECT count(*) FROM alert_rules WHERE kind = 'exposed_port'`); got != 0 {
		t.Error("las reglas exposed_port deben borrarse en el down")
	}
	if got := count(t, `SELECT count(*) FROM information_schema.views WHERE table_name = 'alert_candidates'`); got != 1 {
		t.Error("la vista alert_candidates debe existir tras el down")
	}
	if err := Migrate(ctx, testPool); err != nil {
		t.Fatalf("up tras down: %v", err)
	}
}
