package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// seedTargetFleet crea el escenario comun: el colector col, dos agentes (a1, a2) y dos dispositivos (d1, d2) del grupo
// g (col tambien esta en g: un colector solo sondea equipos de su grupo), todos caidos desde hace un dia. Devuelve el grupo.
func seedTargetFleet(t *testing.T, s *Store) Group {
	t.Helper()
	ctx := context.Background()
	g := mkGroup(t, s, "g", false, nil)
	seedHost(t, "col", "up", ago(time.Second))
	setGroupOf(t, "col", g.ID)
	for _, n := range []string{"a1", "a2"} {
		seedHost(t, n, "down", ago(time.Hour))
		if _, err := s.SetHostGroup(ctx, hostID(t, n), &g.ID); err != nil {
			t.Fatal(err)
		}
	}
	mkDevice(t, s, "d1", "10.0.0.1", "col", &g.ID)
	mkDevice(t, s, "d2", "10.0.0.2", "col", &g.ID)
	mustExec(t, `UPDATE hosts SET status = 'down', status_changed_at = now() - interval '1 day' WHERE hostname IN ('a1', 'a2', 'd1', 'd2')`)
	return g
}

func hostScope(t *testing.T, name string) AlertScope {
	t.Helper()
	id := hostID(t, name)
	return AlertScope{Type: ScopeHost, HostID: &id}
}

func deviceScope(t *testing.T, name string) AlertScope {
	t.Helper()
	id := hostID(t, name)
	return AlertScope{Type: ScopeDevice, HostID: &id}
}

func statusRule(name string, sc AlertScope) AlertRule {
	return AlertRule{Name: name, Kind: "host_status", Scope: sc, Severity: "critical", Enabled: true}
}

// queryStrings ejecuta una consulta de una columna de texto y devuelve los valores.
func queryStrings(t *testing.T, sql string) []string {
	t.Helper()
	rows, err := testPool.Query(context.Background(), sql)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// alertHosts devuelve "regla/host" de cada alerta, ordenado.
func alertHosts(t *testing.T) []string {
	t.Helper()
	return queryStrings(t, `SELECT r.name || '/' || h.hostname FROM alerts a JOIN alert_rules r ON r.id = a.rule_id JOIN hosts h ON h.id = a.host_id ORDER BY 1`)
}

func TestRuleScopeHostAndDeviceFiresOnlyThere(t *testing.T) {
	s := newTestStore(t)
	seedTargetFleet(t, s)
	mkRule(t, s, statusRule("r-host", hostScope(t, "a1")))
	mkRule(t, s, statusRule("r-dev", deviceScope(t, "d1")))
	evalOnce(t, s)
	if got, want := alertHosts(t), []string{"r-dev/d1", "r-host/a1"}; !slices.Equal(got, want) {
		t.Errorf("alertas got %v, want %v (a2 y d2 son del mismo grupo y no deben disparar)", got, want)
	}
}

func TestRuleScopeHostMetricLooksOnlyAtThatHost(t *testing.T) {
	s := newTestStore(t)
	seedTargetFleet(t, s)
	mustExec(t, `UPDATE hosts SET status = 'up' WHERE hostname IN ('a1', 'a2')`)
	for _, n := range []string{"a1", "a2"} {
		setLatest(t, n, 95)
	}
	m := metricRule("cpu", "cpu_pct", "gt", 90, 0)
	m.Scope = hostScope(t, "a2")
	mkRule(t, s, m)
	evalOnce(t, s)
	if got, want := alertHosts(t), []string{"cpu/a2"}; !slices.Equal(got, want) {
		t.Errorf("alertas got %v, want %v (a1 incumple igual pero no es el host de la regla)", got, want)
	}
}

func TestMaintenanceScopeHostAndDeviceSuppressesOnlyThat(t *testing.T) {
	s := newTestStore(t)
	seedTargetFleet(t, s)
	ctx := context.Background()
	mkRule(t, s, statusRule("caido", AlertScope{Type: ScopeAll}))
	now := time.Now()
	for _, sc := range []AlertScope{hostScope(t, "a1"), deviceScope(t, "d1")} {
		w := MaintenanceWindow{Name: "m-" + sc.Type, StartsAt: now.Add(-time.Minute), EndsAt: now.Add(time.Hour), Scope: sc}
		if _, err := s.CreateMaintenance(ctx, w, "", ""); err != nil {
			t.Fatalf("ventana %s: %v", sc.Type, err)
		}
	}
	evalOnce(t, s)
	got := queryStrings(t, `SELECT h.hostname FROM alerts a JOIN hosts h ON h.id = a.host_id WHERE a.suppressed ORDER BY 1`)
	if want := []string{"a1", "d1"}; !slices.Equal(got, want) {
		t.Errorf("suprimidas got %v, want %v", got, want)
	}
	if n := count(t, `SELECT count(*) FROM alerts`); n != 4 {
		t.Errorf("alertas got %d, want 4 (la ventana suprime notificar, no abrir)", n)
	}
}

func TestRuleAndWindowScopeTargetsCRUDAndErrors(t *testing.T) {
	s := newTestStore(t)
	seedTargetFleet(t, s)
	ctx := context.Background()
	a1, d1 := hostScope(t, "a1"), deviceScope(t, "d1")

	r := mkRule(t, s, statusRule("r", a1))
	if r.Scope.Type != ScopeHost || r.Scope.HostID == nil || *r.Scope.HostID != *a1.HostID || r.HostName == nil || *r.HostName != "a1" || r.Scope.GroupID != nil {
		t.Fatalf("lectura del alcance host got %+v name %v", r.Scope, r.HostName)
	}
	// PATCH: host -> device -> all limpia y fija scope_host_id.
	for _, sc := range []AlertScope{d1, {Type: ScopeAll}} {
		got, err := s.UpdateAlertRule(ctx, r.ID, func(x *AlertRule) error { x.Scope = sc; return nil })
		if err != nil || got.Scope.Type != sc.Type || (sc.Type == ScopeAll && (got.Scope.HostID != nil || got.HostName != nil)) {
			t.Fatalf("cambio a %s got (%+v, %v)", sc.Type, got.Scope, err)
		}
		if sc.Type == ScopeDevice && (got.HostName == nil || *got.HostName != "d1") {
			t.Errorf("nombre del dispositivo got %v", got.HostName)
		}
	}

	// Errores de dominio: un dispositivo no vale como host, un agente no vale como dispositivo, un id inexistente no existe.
	ghost := "00000000-0000-4000-8000-000000000000"
	bad := []struct {
		name string
		sc   AlertScope
		want error
	}{
		{"host que es un dispositivo", hostScope(t, "d1"), ErrScopeHostNotFound},
		{"host inexistente", AlertScope{Type: ScopeHost, HostID: &ghost}, ErrScopeHostNotFound},
		{"dispositivo que es un agente", deviceScope(t, "a1"), ErrDeviceNotFound},
		{"dispositivo inexistente", AlertScope{Type: ScopeDevice, HostID: &ghost}, ErrDeviceNotFound},
	}
	for _, tt := range bad {
		if _, err := s.CreateAlertRule(ctx, statusRule("x", tt.sc), ""); !errors.Is(err, tt.want) {
			t.Errorf("regla, %s: got %v, want %v", tt.name, err, tt.want)
		}
		w := MaintenanceWindow{Name: "x", StartsAt: time.Now(), EndsAt: time.Now().Add(time.Hour), Scope: tt.sc}
		if _, err := s.CreateMaintenance(ctx, w, "", ""); !errors.Is(err, tt.want) {
			t.Errorf("ventana, %s: got %v, want %v", tt.name, err, tt.want)
		}
	}
	// Combinaciones que solo la BD frena (la API las responde 400 antes): device con metric, host con service_check.
	m := metricRule("cpu", "cpu_pct", "gt", 90, 0)
	m.Scope = d1
	if _, err := s.CreateAlertRule(ctx, m, ""); err == nil {
		t.Error("device + metric debio rechazarlo la BD")
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO alert_rules (name, kind, scope_type, scope_host_id, check_id, severity)
		VALUES ('sc', 'service_check', 'host', $1, gen_random_uuid(), 'warn')`, *a1.HostID); err == nil {
		t.Error("host + service_check debio rechazarlo la BD")
	}

	// Ventana: crear, leer con nombre y PATCH a otro alcance.
	now := time.Now()
	w, err := s.CreateMaintenance(ctx, MaintenanceWindow{Name: "w", StartsAt: now, EndsAt: now.Add(time.Hour), Scope: d1}, "", "")
	if err != nil || w.HostName == nil || *w.HostName != "d1" || w.Scope.Type != ScopeDevice {
		t.Fatalf("ventana device got (%+v, %v)", w, err)
	}
	got, err := s.UpdateMaintenance(ctx, w.ID, func(x *MaintenanceWindow) error { x.Scope = a1; return nil })
	if err != nil || got.Scope.Type != ScopeHost || got.HostName == nil || *got.HostName != "a1" {
		t.Fatalf("ventana device -> host got (%+v, %v)", got, err)
	}

	// Borrar el host borra sus reglas y ventanas propias (FK CASCADE).
	r2 := mkRule(t, s, statusRule("r2", a1))
	mustExec(t, `DELETE FROM hosts WHERE hostname = 'a1'`)
	if _, err := s.AlertRuleByID(ctx, AllScope(), r2.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("regla del host borrado got %v, want ErrNotFound", err)
	}
	if _, err := s.MaintenanceByID(ctx, AllScope(), w.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("ventana del host borrado got %v, want ErrNotFound", err)
	}
}

func TestRuleAndWindowScopeTargetsRestrictedScope(t *testing.T) {
	s := newTestStore(t)
	g := seedTargetFleet(t, s)
	ctx := context.Background()
	other := mkGroup(t, s, "other", false, nil)
	seedHost(t, "foreign", "up", ago(time.Second))
	if _, err := s.SetHostGroup(ctx, hostID(t, "foreign"), &other.ID); err != nil {
		t.Fatal(err)
	}
	seedHost(t, "loose", "up", ago(time.Second)) // sin grupo
	own := mkRule(t, s, statusRule("own", hostScope(t, "a1")))
	ownDev := mkRule(t, s, statusRule("own-dev", deviceScope(t, "d1")))
	foreign := mkRule(t, s, statusRule("foreign", hostScope(t, "foreign")))
	loose := mkRule(t, s, statusRule("loose", hostScope(t, "loose")))
	viewer := GroupScope([]string{g.ID})

	rules, err := s.ListAlertRules(ctx, viewer)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range rules {
		names = append(names, r.Name)
	}
	if want := []string{"own", "own-dev"}; !slices.Equal(names, want) {
		t.Errorf("reglas visibles got %v, want %v", names, want)
	}
	for _, id := range []string{own.ID, ownDev.ID} {
		if _, err := s.AlertRuleByID(ctx, viewer, id); err != nil {
			t.Errorf("regla propia: %v", err)
		}
	}
	for _, id := range []string{foreign.ID, loose.ID} {
		if _, err := s.AlertRuleByID(ctx, viewer, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("regla ajena got %v, want ErrNotFound", err)
		}
	}

	now := time.Now()
	mk := func(name string, sc AlertScope) MaintenanceWindow {
		w, err := s.CreateMaintenance(ctx, MaintenanceWindow{Name: name, StartsAt: now, EndsAt: now.Add(time.Hour), Scope: sc}, "", "")
		if err != nil {
			t.Fatal(err)
		}
		return w
	}
	wOwn, wForeign := mk("w-own", hostScope(t, "a1")), mk("w-foreign", hostScope(t, "foreign"))
	ws, err := s.ListMaintenance(ctx, viewer)
	if err != nil || len(ws) != 1 || ws[0].ID != wOwn.ID {
		t.Errorf("ventanas visibles got (%v, %v), want solo w-own", ws, err)
	}
	if _, err := s.MaintenanceByID(ctx, viewer, wForeign.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("ventana ajena got %v, want ErrNotFound", err)
	}
	if _, err := s.MaintenanceByID(ctx, viewer, wOwn.ID); err != nil {
		t.Errorf("ventana propia: %v", err)
	}
}

// TestMigrationScopeTargetsDown comprueba que Down (00029 -> 28) borra las reglas y ventanas con alcance host o
// dispositivo sin tocar las demas y que Up las vuelve a permitir. Deja la BD en el ultimo esquema.
func TestMigrationScopeTargetsDown(t *testing.T) {
	s := newTestStore(t)
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

	seedTargetFleet(t, s)
	mkRule(t, s, statusRule("r-host", hostScope(t, "a1")))
	mkRule(t, s, statusRule("r-dev", deviceScope(t, "d1")))
	mkRule(t, s, statusRule("r-all", AlertScope{Type: ScopeAll}))
	now := time.Now()
	for i, sc := range []AlertScope{hostScope(t, "a1"), {Type: ScopeAll}} {
		w := MaintenanceWindow{Name: fmt.Sprintf("w%d", i), StartsAt: now, EndsAt: now.Add(time.Hour), Scope: sc}
		if _, err := s.CreateMaintenance(ctx, w, "", ""); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := provider.DownTo(ctx, 28); err != nil {
		t.Fatalf("down a 28: %v", err)
	}
	if n := count(t, `SELECT count(*) FROM alert_rules`); n != 1 {
		t.Errorf("reglas tras el down got %d, want 1 (solo r-all)", n)
	}
	if n := count(t, `SELECT count(*) FROM maintenance_windows`); n != 1 {
		t.Errorf("ventanas tras el down got %d, want 1 (solo la de all)", n)
	}
	if n := count(t, `SELECT count(*) FROM information_schema.columns WHERE column_name = 'scope_host_id' AND table_name IN ('alert_rules', 'maintenance_windows')`); n != 0 {
		t.Errorf("scope_host_id debe haberse eliminado, quedan %d", n)
	}
	if _, err := s.EvaluateAlerts(ctx); err != nil {
		t.Errorf("la vista restaurada debe evaluar: %v", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("up tras down: %v", err)
	}
	if _, err := s.CreateAlertRule(ctx, statusRule("r-host2", hostScope(t, "a1")), ""); err != nil {
		t.Errorf("tras Up el alcance host debe volver a admitirse: %v", err)
	}
}
