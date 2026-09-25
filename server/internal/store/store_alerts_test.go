package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Tests del motor de alertas contra Postgres real. El evaluador usa now() de la BD: pa avanzar el tiempo se
// retrasan a mano las marcas (alert_pending.since, alerts.clear_since, opened_at) en vez de dormir.

func sptr(s string) *string { return &s }

// metricRule arma una regla de metrica (alcance todos, sin histeresis salvo que se cambie).
func metricRule(name, metric, op string, threshold float64, durationSec int) AlertRule {
	return AlertRule{
		Name: name, Kind: "metric", Metric: sptr(metric), Op: sptr(op), Threshold: ptr(threshold),
		DurationSeconds: durationSec, ResolveSeconds: 0, Scope: AlertScope{Type: ScopeAll},
		Severity: "warn", Enabled: true,
	}
}

func mkRule(t *testing.T, s *Store, r AlertRule) AlertRule {
	t.Helper()
	got, err := s.CreateAlertRule(context.Background(), r, "")
	if err != nil {
		t.Fatalf("creando regla %q: %v", r.Name, err)
	}
	return got
}

// mkChannel crea un canal de prueba y lo asigna a la regla.
func mkChannel(t *testing.T, name, minSeverity, ruleID string) string {
	t.Helper()
	var id string
	err := testPool.QueryRow(context.Background(),
		`INSERT INTO notification_channels (name, kind, min_severity) VALUES ($1, 'webhook', $2) RETURNING id`,
		name, minSeverity).Scan(&id)
	if err != nil {
		t.Fatalf("creando canal: %v", err)
	}
	if ruleID != "" {
		mustExec(t, `INSERT INTO rule_channels (rule_id, channel_id) VALUES ($1, $2)`, ruleID, id)
	}
	return id
}

// setLatest fija el ultimo valor de un host con ts = now() (memoria 1000/2000 MB).
func setLatest(t *testing.T, name string, cpu float64) {
	t.Helper()
	mustExec(t,
		`INSERT INTO host_latest (host_id, ts, cpu_pct, mem_used_mb, mem_total_mb) VALUES ($1, now(), $2, 1000, 2000)
		 ON CONFLICT (host_id) DO UPDATE SET ts = now(), cpu_pct = EXCLUDED.cpu_pct`, hostID(t, name), cpu)
}

func evalOnce(t *testing.T, s *Store) EvalResult {
	t.Helper()
	res, err := s.EvaluateAlerts(context.Background())
	if err != nil {
		t.Fatalf("EvaluateAlerts: %v", err)
	}
	if !res.Ran {
		t.Fatal("el evaluador no corrio (lock ocupado)")
	}
	return res
}

func backdatePending(t *testing.T, d string) {
	t.Helper()
	mustExec(t, `UPDATE alert_pending SET since = now() - $1::interval`, d)
}

type alertRow struct {
	State, Reason, Severity string
	Trigger, Peak           *float64
	Suppressed              bool
	ClearSince              *time.Time
}

// oneAlert devuelve la unica alerta de la tabla (falla si hay 0 o mas de 1).
func oneAlert(t *testing.T) alertRow {
	t.Helper()
	if n := count(t, `SELECT count(*) FROM alerts`); n != 1 {
		t.Fatalf("alertas got %d, want 1", n)
	}
	var a alertRow
	var reason *string
	err := testPool.QueryRow(context.Background(),
		`SELECT state, resolve_reason, severity, trigger_value, peak_value, suppressed, clear_since FROM alerts`).
		Scan(&a.State, &reason, &a.Severity, &a.Trigger, &a.Peak, &a.Suppressed, &a.ClearSince)
	if err != nil {
		t.Fatal(err)
	}
	if reason != nil {
		a.Reason = *reason
	}
	return a
}

func deliveries(t *testing.T, event, status string) int {
	return count(t, `SELECT count(*) FROM notification_deliveries WHERE event = $1 AND status = $2`, event, status)
}

func TestEvaluateMetricLifecycleWithHysteresis(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "srv", "up", ago(time.Second))
	rule := metricRule("cpu", "cpu_pct", "gt", 90, 600)
	rule.ClearThreshold = ptr(80)
	rule.ResolveSeconds = 120
	rule = mkRule(t, s, rule)
	mkChannel(t, "hook", "warn", rule.ID)

	// Incumple: queda pendiente, aun sin alerta.
	setLatest(t, "srv", 95)
	if r := evalOnce(t, s); r.Opened != 0 || count(t, `SELECT count(*) FROM alert_pending`) != 1 || count(t, `SELECT count(*) FROM alerts`) != 0 {
		t.Fatalf("primer ciclo got %+v, want solo pendiente", r)
	}
	// El pendiente no se reinicia entre ciclos.
	backdatePending(t, "11 minutes")
	evalOnce(t, s)
	if r := evalOnce(t, s); r.Opened != 0 {
		t.Errorf("no debe duplicar: %+v", r)
	}
	a := oneAlert(t)
	if a.State != "open" || a.Trigger == nil || *a.Trigger != 95 || a.Suppressed {
		t.Fatalf("alerta got %+v", a)
	}
	if got := deliveries(t, "opened", "pending"); got != 1 {
		t.Errorf("entregas opened got %d, want 1", got)
	}
	if count(t, `SELECT count(*) FROM alert_pending`) != 0 {
		t.Error("el pendiente debe borrarse al abrir")
	}

	// Pico, y banda de histeresis (85 esta entre clear 80 y umbral 90): no cuenta como sano.
	setLatest(t, "srv", 99)
	evalOnce(t, s)
	setLatest(t, "srv", 85)
	evalOnce(t, s)
	if a := oneAlert(t); a.Peak == nil || *a.Peak != 99 || a.ClearSince != nil || a.State != "open" {
		t.Fatalf("en banda got %+v", a)
	}

	// Sano (<= 80): empieza clear_since pero aun no resuelve (resolve_seconds 120).
	setLatest(t, "srv", 50)
	evalOnce(t, s)
	if a := oneAlert(t); a.ClearSince == nil || a.State != "open" {
		t.Fatalf("sano got %+v", a)
	}
	// Un rebote lo reinicia.
	setLatest(t, "srv", 95)
	evalOnce(t, s)
	if a := oneAlert(t); a.ClearSince != nil {
		t.Fatalf("rebote debe limpiar clear_since: %+v", a)
	}
	setLatest(t, "srv", 50)
	evalOnce(t, s)
	mustExec(t, `UPDATE alerts SET clear_since = now() - interval '3 minutes'`)
	if r := evalOnce(t, s); r.Resolved != 1 {
		t.Fatalf("resolved got %d, want 1", r.Resolved)
	}
	if a := oneAlert(t); a.State != "resolved" || a.Reason != "recovered" {
		t.Fatalf("alerta got %+v", a)
	}
	if got := deliveries(t, "resolved", "pending"); got != 1 {
		t.Errorf("entregas resolved got %d, want 1", got)
	}
	// Idempotente: otro ciclo no agrega nada.
	evalOnce(t, s)
	if n := count(t, `SELECT count(*) FROM notification_deliveries`); n != 2 {
		t.Errorf("entregas totales got %d, want 2", n)
	}
}

func TestEvaluateBelowThresholdRuleAndBatteryCharging(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "lap", "up", ago(time.Second))
	rule := mkRule(t, s, metricRule("bat", "battery_pct", "lt", 10, 0))
	mustExec(t, `INSERT INTO host_latest (host_id, ts, cpu_pct, mem_used_mb, mem_total_mb, battery_pct, battery_charging)
		VALUES ($1, now(), 1, 1, 2, 5, true)`, hostID(t, "lap"))

	// Cargando: no incumple aunque la bateria sea 5 %.
	evalOnce(t, s)
	if count(t, `SELECT count(*) FROM alert_pending`)+count(t, `SELECT count(*) FROM alerts`) != 0 {
		t.Fatal("una bateria cargando no debe alertar")
	}
	mustExec(t, `UPDATE host_latest SET battery_charging = false`)
	if r := evalOnce(t, s); r.Opened != 1 {
		t.Fatalf("sin cargar debe abrir: %+v", r)
	}
	// Al enchufarla, se resuelve (resolve_seconds 0).
	mustExec(t, `UPDATE host_latest SET battery_charging = true`)
	evalOnce(t, s)
	if a := oneAlert(t); a.State != "resolved" {
		t.Fatalf("al cargar debe resolver: %+v", a)
	}
	_ = rule
}

func TestEvaluateMemPctIsComputed(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "srv", "up", ago(time.Second))
	mkRule(t, s, metricRule("mem", "mem_pct", "gt", 40, 0))
	setLatest(t, "srv", 1) // memoria 1000/2000 = 50 %
	if r := evalOnce(t, s); r.Opened != 1 {
		t.Fatalf("mem 50%% > 40%% debe abrir: %+v", r)
	}
	if a := oneAlert(t); a.Trigger == nil || *a.Trigger != 50 {
		t.Errorf("trigger got %+v, want 50", a.Trigger)
	}
}

func TestEvaluateNoChannelsEnqueuesNothing(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "srv", "up", ago(time.Second))
	mkRule(t, s, metricRule("cpu", "cpu_pct", "gt", 90, 0))
	setLatest(t, "srv", 95)
	if r := evalOnce(t, s); r.Opened != 1 {
		t.Fatalf("opened got %d", r.Opened)
	}
	if n := count(t, `SELECT count(*) FROM notification_deliveries`); n != 0 {
		t.Errorf("sin canales no se encola nada, got %d", n)
	}
	// Manual resolve sin canales tampoco encola.
	var id string
	if err := testPool.QueryRow(context.Background(), `SELECT id FROM alerts`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	uid := mkUser(t, s, "adm", "admin")
	if _, err := s.ResolveAlert(context.Background(), id, uid, "adm"); err != nil {
		t.Fatal(err)
	}
	if n := count(t, `SELECT count(*) FROM notification_deliveries`); n != 0 {
		t.Errorf("resolve sin canales encolo %d", n)
	}
}

func TestEvaluateChannelMinSeverityAndDisabled(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "srv", "up", ago(time.Second))
	rule := metricRule("cpu", "cpu_pct", "gt", 90, 0)
	rule.Severity = "warn"
	rule = mkRule(t, s, rule)
	mkChannel(t, "solo-criticas", "critical", rule.ID)
	off := mkChannel(t, "apagado", "info", rule.ID)
	mustExec(t, `UPDATE notification_channels SET enabled = false WHERE id = $1`, off)
	ok := mkChannel(t, "warn", "warn", rule.ID)
	setLatest(t, "srv", 95)
	evalOnce(t, s)
	if n := count(t, `SELECT count(*) FROM notification_deliveries`); n != 1 {
		t.Fatalf("entregas got %d, want 1", n)
	}
	if n := count(t, `SELECT count(*) FROM notification_deliveries WHERE channel_id = $1`, ok); n != 1 {
		t.Error("la entrega debe ir al canal warn")
	}
}

func TestEvaluateOfflineAndStaleHosts(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "lap", "offline", ago(time.Hour))
	seedHost(t, "stale", "up", ago(time.Second))
	seedHost(t, "dis", "disabled", ago(time.Hour))
	seedHost(t, "pend", "pending", ago(time.Hour))
	mkRule(t, s, metricRule("cpu", "cpu_pct", "gt", 90, 600))
	setLatest(t, "lap", 99)
	setLatest(t, "dis", 99)
	setLatest(t, "pend", 99)
	setLatest(t, "stale", 99)
	mustExec(t, `UPDATE host_latest SET ts = now() - interval '10 minutes' WHERE host_id = $1`, hostID(t, "stale"))

	evalOnce(t, s)
	if n := count(t, `SELECT count(*) FROM alert_pending`); n != 0 {
		t.Fatalf("offline/disabled/pending/dato viejo no evaluan, pendientes got %d", n)
	}

	// include_offline: el offline si se evalua con su ultimo valor conocido; disabled y pending siguen fuera.
	inc := metricRule("cpu-off", "cpu_pct", "gt", 90, 0)
	inc.IncludeOffline = true
	mkRule(t, s, inc)
	if r := evalOnce(t, s); r.Opened != 2 { // lap (offline) y stale (up con dato viejo)
		t.Fatalf("include_offline opened got %d, want 2", r.Opened)
	}
	if n := count(t, `SELECT count(*) FROM alerts a JOIN hosts h ON h.id = a.host_id WHERE h.hostname IN ('dis', 'pend')`); n != 0 {
		t.Errorf("disabled/pending no deben alertar, got %d", n)
	}
}

func TestOpenAlertOnHostGoingOfflineStaysOpenWithoutNotifying(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "srv", "up", ago(time.Second))
	rule := metricRule("cpu", "cpu_pct", "gt", 90, 0)
	rule.Severity = "critical"
	rule = mkRule(t, s, rule)
	mkChannel(t, "hook", "info", rule.ID)
	setLatest(t, "srv", 95)
	evalOnce(t, s)
	mustExec(t, `UPDATE notification_deliveries SET status = 'sent'`)
	mustExec(t, `UPDATE alerts SET opened_at = now() - interval '9 hours'`)

	// El host pasa a offline: la alerta se mantiene, no se resuelve ni se recuerda.
	mustExec(t, `UPDATE hosts SET status = 'offline'`)
	evalOnce(t, s)
	evalOnce(t, s)
	if a := oneAlert(t); a.State != "open" {
		t.Fatalf("debe seguir abierta: %+v", a)
	}
	if n := count(t, `SELECT count(*) FROM notification_deliveries`); n != 1 {
		t.Errorf("host offline no notifica (entregas got %d, want 1)", n)
	}
}

func TestEvaluateHostStatusRule(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "srv", "down", ago(time.Hour)) // status_changed_at = hace 1 dia
	seedHost(t, "lap", "offline", ago(time.Hour))
	rule := AlertRule{Name: "caido", Kind: "host_status", DurationSeconds: 120, ResolveSeconds: 0,
		Scope: AlertScope{Type: ScopeAll}, Severity: "critical", Enabled: true}
	mkRule(t, s, rule)

	// El inicio real es status_changed_at (hace 1 dia): abre en el primer ciclo. El offline no dispara.
	if r := evalOnce(t, s); r.Opened != 1 {
		t.Fatalf("opened got %d, want 1", r.Opened)
	}
	if a := oneAlert(t); a.State != "open" || a.Severity != "critical" {
		t.Fatalf("alerta got %+v", a)
	}
	// Recupera: resuelve.
	mustExec(t, `UPDATE hosts SET status = 'up' WHERE hostname = 'srv'`)
	evalOnce(t, s)
	if a := oneAlert(t); a.State != "resolved" || a.Reason != "recovered" {
		t.Fatalf("alerta got %+v", a)
	}
}

func TestEvaluateHostStatusRespectsDuration(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "srv", "down", ago(time.Hour))
	mustExec(t, `UPDATE hosts SET status_changed_at = now() - interval '30 seconds'`)
	mkRule(t, s, AlertRule{Name: "caido", Kind: "host_status", DurationSeconds: 120,
		Scope: AlertScope{Type: ScopeAll}, Severity: "critical", Enabled: true})
	if r := evalOnce(t, s); r.Opened != 0 {
		t.Fatal("solo lleva 30 s caido: no debe abrir")
	}
	mustExec(t, `UPDATE hosts SET status_changed_at = now() - interval '3 minutes'`)
	backdatePending(t, "3 minutes")
	if r := evalOnce(t, s); r.Opened != 1 {
		t.Fatal("ya cumplio la duracion: debe abrir")
	}
}

func TestEvaluateNoDataRule(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "srv", "up", ago(5*time.Minute)) // el detector aun no lo marco
	seedHost(t, "ok", "up", ago(10*time.Second))
	seedHost(t, "lap", "offline", ago(5*time.Minute))
	mkRule(t, s, AlertRule{Name: "sin datos", Kind: "no_data", Threshold: ptr(120), DurationSeconds: 0, ResolveSeconds: 0,
		Scope: AlertScope{Type: ScopeAll}, Severity: "warn", Enabled: true})
	if r := evalOnce(t, s); r.Opened != 1 {
		t.Fatalf("opened got %d, want 1 (solo srv)", r.Opened)
	}
	if a := oneAlert(t); a.Trigger == nil || *a.Trigger < 290 {
		t.Errorf("trigger (segundos sin datos) got %+v", a.Trigger)
	}
	mustExec(t, `UPDATE hosts SET last_seen_at = now() WHERE hostname = 'srv'`)
	evalOnce(t, s)
	if a := oneAlert(t); a.State != "resolved" {
		t.Fatalf("al reportar debe resolver: %+v", a)
	}
}

func TestEvaluateScopeGroup(t *testing.T) {
	s := newTestStore(t)
	g := mkGroup(t, s, "prod", false, nil)
	for _, n := range []string{"a", "b", "c"} {
		seedHost(t, n, "up", ago(time.Second))
		setLatest(t, n, 99)
	}
	setGroupOf(t, "a", g.ID)

	byGroup := metricRule("g", "cpu_pct", "gt", 90, 0)
	byGroup.Scope = AlertScope{Type: ScopeGroup, GroupID: &g.ID}
	mkRule(t, s, byGroup)
	if r := evalOnce(t, s); r.Opened != 1 {
		t.Fatalf("alcance grupo opened got %d, want 1", r.Opened)
	}
	if n := count(t, `SELECT count(*) FROM alerts a JOIN hosts h ON h.id = a.host_id WHERE h.hostname <> 'a'`); n != 0 {
		t.Error("el host fuera de alcance no debe alertar")
	}
}

func TestEvaluateMaintenanceSuppressesThenReleases(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "srv", "up", ago(time.Second))
	rule := metricRule("cpu", "cpu_pct", "gt", 90, 0)
	rule.Severity = "critical"
	rule = mkRule(t, s, rule)
	mkChannel(t, "hook", "info", rule.ID)
	mustExec(t, `INSERT INTO maintenance_windows (name, starts_at, ends_at, scope_type)
		VALUES ('parche', now() - interval '1 hour', now() + interval '1 hour', 'all')`)
	setLatest(t, "srv", 95)

	// La alerta se abre y es visible, pero su notificacion queda suprimida.
	if r := evalOnce(t, s); r.Opened != 1 {
		t.Fatalf("opened got %d", r.Opened)
	}
	if a := oneAlert(t); !a.Suppressed || a.State != "open" {
		t.Fatalf("alerta got %+v", a)
	}
	if deliveries(t, "opened", "suppressed") != 1 || deliveries(t, "opened", "pending") != 0 {
		t.Fatal("la entrega opened debe quedar suppressed")
	}
	// Sigue en mantenimiento: no se libera.
	if r := evalOnce(t, s); r.Released != 0 {
		t.Fatal("no debe liberar dentro de la ventana")
	}
	// Termina la ventana: se libera y la notificacion sale.
	mustExec(t, `DELETE FROM maintenance_windows`)
	if r := evalOnce(t, s); r.Released != 1 {
		t.Fatalf("released got %d, want 1", r.Released)
	}
	if a := oneAlert(t); a.Suppressed {
		t.Fatal("ya no debe estar suppressed")
	}
	if deliveries(t, "opened", "pending") != 1 {
		t.Fatal("la entrega opened debe pasar a pending")
	}
}

func TestEvaluateMaintenanceScopedToGroup(t *testing.T) {
	s := newTestStore(t)
	g := mkGroup(t, s, "prod", false, nil)
	seedHost(t, "in", "up", ago(time.Second))
	seedHost(t, "out", "up", ago(time.Second))
	setGroupOf(t, "in", g.ID)
	setLatest(t, "in", 99)
	setLatest(t, "out", 99)
	mkRule(t, s, metricRule("cpu", "cpu_pct", "gt", 90, 0))
	mustExec(t, `INSERT INTO maintenance_windows (name, starts_at, ends_at, scope_type, scope_group_id)
		VALUES ('g', now() - interval '1 hour', now() + interval '1 hour', 'group', $1)`, g.ID)
	evalOnce(t, s)
	if n := count(t, `SELECT count(*) FROM alerts a JOIN hosts h ON h.id = a.host_id WHERE a.suppressed AND h.hostname = 'in'`); n != 1 {
		t.Error("el host del grupo debe quedar suppressed")
	}
	if n := count(t, `SELECT count(*) FROM alerts a JOIN hosts h ON h.id = a.host_id WHERE NOT a.suppressed AND h.hostname = 'out'`); n != 1 {
		t.Error("el host fuera del grupo no se suprime")
	}
}

func TestEvaluateResolvesOrphans(t *testing.T) {
	tests := []struct {
		name   string
		mutate string
		want   string
	}{
		{"regla desactivada", `UPDATE alert_rules SET enabled = false`, "rule_disabled"},
		{"regla borrada", `DELETE FROM alert_rules`, "rule_deleted"},
		{"host deshabilitado", `UPDATE hosts SET status = 'disabled', agent_token_revoked_at = now()`, "host_disabled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			seedHost(t, "srv", "up", ago(time.Second))
			rule := mkRule(t, s, metricRule("cpu", "cpu_pct", "gt", 90, 0))
			mkChannel(t, "hook", "info", rule.ID)
			setLatest(t, "srv", 95)
			evalOnce(t, s)
			mustExec(t, `UPDATE notification_deliveries SET status = 'sent'`)
			mustExec(t, tt.mutate)
			if r := evalOnce(t, s); r.Resolved != 1 {
				t.Fatalf("resolved got %d, want 1", r.Resolved)
			}
			if a := oneAlert(t); a.State != "resolved" || a.Reason != tt.want {
				t.Fatalf("alerta got %+v, want reason %s", a, tt.want)
			}
			if deliveries(t, "resolved", "pending") != 1 {
				t.Error("debe avisar la resolucion a quien recibio el opened")
			}
		})
	}
}

func TestResolvedNotificationOnlyToChannelsThatGotOpened(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "srv", "up", ago(time.Second))
	rule := mkRule(t, s, metricRule("cpu", "cpu_pct", "gt", 90, 0))
	mkChannel(t, "hook", "info", rule.ID)
	setLatest(t, "srv", 95)
	evalOnce(t, s)
	mustExec(t, `UPDATE notification_deliveries SET status = 'suppressed'`) // nunca se anuncio
	setLatest(t, "srv", 10)
	evalOnce(t, s)
	if oneAlert(t).State != "resolved" || deliveries(t, "resolved", "pending") != 0 {
		t.Error("no se anuncia la resolucion de algo que nunca se anuncio")
	}
}

func TestEvaluateReminders(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "srv", "up", ago(time.Second))
	rule := metricRule("cpu", "cpu_pct", "gt", 90, 0)
	rule.Severity = "critical"
	rule = mkRule(t, s, rule)
	mkChannel(t, "hook", "info", rule.ID)
	setLatest(t, "srv", 95)
	evalOnce(t, s)
	mustExec(t, `UPDATE notification_deliveries SET status = 'sent'`)

	// Menos de 4 h: sin recordatorio.
	evalOnce(t, s)
	if n := count(t, `SELECT count(*) FROM notification_deliveries WHERE event = 'reminder'`); n != 0 {
		t.Fatalf("recordatorios got %d, want 0", n)
	}
	mustExec(t, `UPDATE alerts SET opened_at = now() - interval '4 hours 5 minutes'`)
	if r := evalOnce(t, s); r.Reminders != 1 {
		t.Fatalf("reminders got %d, want 1", r.Reminders)
	}
	// Idempotente dentro del mismo tramo de 4 h; el siguiente tramo genera otro.
	if r := evalOnce(t, s); r.Reminders != 0 {
		t.Fatalf("no debe repetir el recordatorio: %+v", r)
	}
	mustExec(t, `UPDATE alerts SET opened_at = now() - interval '8 hours 5 minutes'`)
	evalOnce(t, s)
	if n := count(t, `SELECT count(*) FROM notification_deliveries WHERE event = 'reminder'`); n != 2 {
		t.Fatalf("recordatorios got %d, want 2", n)
	}
	// Reconocida: no se recuerda mas.
	mustExec(t, `UPDATE alerts SET state = 'acknowledged', acknowledged_at = now(), opened_at = now() - interval '13 hours'`)
	evalOnce(t, s)
	if n := count(t, `SELECT count(*) FROM notification_deliveries WHERE event = 'reminder'`); n != 2 {
		t.Errorf("una alerta reconocida no recuerda, got %d", n)
	}
}

func TestRemindersOnlyForCriticalUnacknowledged(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "srv", "up", ago(time.Second))
	rule := mkRule(t, s, metricRule("cpu", "cpu_pct", "gt", 90, 0)) // warn
	mkChannel(t, "hook", "info", rule.ID)
	setLatest(t, "srv", 95)
	evalOnce(t, s)
	mustExec(t, `UPDATE notification_deliveries SET status = 'sent'`)
	mustExec(t, `UPDATE alerts SET opened_at = now() - interval '9 hours'`)
	evalOnce(t, s)
	if n := count(t, `SELECT count(*) FROM notification_deliveries WHERE event = 'reminder'`); n != 0 {
		t.Errorf("solo las criticas recuerdan, got %d", n)
	}
}

func TestDeliveryQueueCapSuppressesNewOnes(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "srv", "up", ago(time.Second))
	seedHost(t, "srv2", "up", ago(time.Second))
	rule := mkRule(t, s, metricRule("cpu", "cpu_pct", "gt", 90, 0))
	ch := mkChannel(t, "hook", "info", rule.ID)
	// Una alerta ya resuelta con 10 000 entregas pending (seq distinto pa respetar la UNIQUE).
	mustExec(t, `INSERT INTO alerts (id, rule_id, host_id, rule_name, rule_kind, severity, state, resolved_at, resolve_reason)
		VALUES ('00000000-0000-0000-0000-000000000001', NULL, $1, 'x', 'metric', 'warn', 'resolved', now(), 'manual')`, hostID(t, "srv"))
	mustExec(t, `INSERT INTO notification_deliveries (alert_id, channel_id, event, seq)
		SELECT '00000000-0000-0000-0000-000000000001', $1, 'reminder', g FROM generate_series(1, $2::int) g`, ch, MaxPendingDeliveries)
	setLatest(t, "srv2", 95)
	evalOnce(t, s)
	if got := deliveries(t, "opened", "suppressed"); got != 1 {
		t.Errorf("con la cola llena la entrega debe quedar suppressed, got %d", got)
	}
	if got := deliveries(t, "opened", "pending"); got != 0 {
		t.Errorf("pending got %d, want 0", got)
	}
}

func TestEvaluateSkipsWhenAnotherInstanceHoldsTheLock(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // el rollback tras Commit devuelve ErrTxClosed: nada que hacer
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, alertEvalLockName); err != nil {
		t.Fatal(err)
	}
	res, err := s.EvaluateAlerts(ctx)
	if err != nil || res.Ran {
		t.Fatalf("con el lock ocupado got %+v, %v; want Ran=false", res, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if res := evalOnce(t, s); !res.Ran {
		t.Fatal("liberado el lock debe correr")
	}
}

func TestEvaluateConcurrentInstancesNeverDuplicate(t *testing.T) {
	s := newTestStore(t)
	for i := range 20 {
		name := "h" + string(rune('a'+i))
		seedHost(t, name, "up", ago(time.Second))
		setLatest(t, name, 99)
	}
	rule := mkRule(t, s, metricRule("cpu", "cpu_pct", "gt", 90, 0))
	mkChannel(t, "hook", "info", rule.ID)

	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			if _, err := s.EvaluateAlerts(context.Background()); err != nil {
				t.Errorf("EvaluateAlerts: %v", err)
			}
		})
	}
	wg.Wait()
	if n := count(t, `SELECT count(*) FROM alerts`); n != 20 {
		t.Errorf("alertas got %d, want 20 (una por host)", n)
	}
	if n := count(t, `SELECT count(*) FROM notification_deliveries`); n != 20 {
		t.Errorf("entregas got %d, want 20", n)
	}
}

func TestActiveAlertUniqueIndexIsTheHardDedupe(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "srv", "up", ago(time.Second))
	rule := mkRule(t, s, metricRule("cpu", "cpu_pct", "gt", 90, 0))
	ins := `INSERT INTO alerts (rule_id, host_id, rule_name, rule_kind, severity) VALUES ($1, $2, 'cpu', 'metric', 'warn')`
	mustExec(t, ins, rule.ID, hostID(t, "srv"))
	_, err := testPool.Exec(context.Background(), ins, rule.ID, hostID(t, "srv"))
	if !isUniqueViolation(err, "alerts_active_rule_host_key") {
		t.Fatalf("segunda alerta activa got %v, want violacion de alerts_active_rule_host_key", err)
	}
	// Una resuelta no bloquea a una nueva.
	mustExec(t, `UPDATE alerts SET state = 'resolved', resolved_at = now(), resolve_reason = 'manual'`)
	mustExec(t, ins, rule.ID, hostID(t, "srv"))
}

func TestAckAndResolveAlert(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedHost(t, "srv", "up", ago(time.Second))
	rule := mkRule(t, s, metricRule("cpu", "cpu_pct", "gt", 90, 0))
	mkChannel(t, "hook", "info", rule.ID)
	setLatest(t, "srv", 95)
	evalOnce(t, s)
	mustExec(t, `UPDATE notification_deliveries SET status = 'sent'`)
	var id string
	if err := testPool.QueryRow(ctx, `SELECT id FROM alerts`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	uid := mkUser(t, s, "adm", "admin")

	a, err := s.AckAlert(ctx, id, uid, "adm")
	if err != nil || a.State != "acknowledged" || a.AcknowledgedByName == nil || *a.AcknowledgedByName != "adm" {
		t.Fatalf("ack got %+v, %v", a, err)
	}
	// Idempotente.
	if a, err := s.AckAlert(ctx, id, uid, "adm"); err != nil || a.State != "acknowledged" {
		t.Fatalf("segundo ack got %+v, %v", a, err)
	}
	a, err = s.ResolveAlert(ctx, id, uid, "adm")
	if err != nil || a.State != "resolved" || a.ResolveReason == nil || *a.ResolveReason != "manual" || a.ResolvedByName == nil {
		t.Fatalf("resolve got %+v, %v", a, err)
	}
	if deliveries(t, "resolved", "pending") != 1 {
		t.Error("resolver a mano avisa a quien recibio el opened")
	}
	if _, err := s.ResolveAlert(ctx, id, uid, "adm"); !errors.Is(err, ErrAlertResolved) {
		t.Errorf("resolver dos veces got %v, want ErrAlertResolved", err)
	}
	if _, err := s.AckAlert(ctx, id, uid, "adm"); !errors.Is(err, ErrAlertResolved) {
		t.Errorf("ack de resuelta got %v, want ErrAlertResolved", err)
	}
	const none = "00000000-0000-0000-0000-0000000000aa"
	if _, err := s.AckAlert(ctx, none, uid, "adm"); !errors.Is(err, ErrNotFound) {
		t.Errorf("ack inexistente got %v, want ErrNotFound", err)
	}
	if _, err := s.ResolveAlert(ctx, none, uid, "adm"); !errors.Is(err, ErrNotFound) {
		t.Errorf("resolve inexistente got %v, want ErrNotFound", err)
	}
	// Tras resolver a mano, si la condicion sigue, el evaluador abre otra alerta (la duracion de la regla es 0).
	if r := evalOnce(t, s); r.Opened != 1 {
		t.Errorf("reapertura got %+v", r)
	}
}

func TestListAlertsFiltersKeysetAndTotal(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, h := range []string{"a", "b", "c"} {
		seedHost(t, h, "up", ago(time.Second))
	}
	r1 := mkRule(t, s, metricRule("r1", "cpu_pct", "gt", 90, 0))
	crit := metricRule("r2", "mem_pct", "gt", 90, 0)
	crit.Severity = "critical"
	r2 := mkRule(t, s, crit)
	// 5 alertas; la 4 y la 5 comparten opened_at (desempata por id). Pares (regla, host) distintos: el indice
	// unico solo admite una activa por par.
	hosts := []string{"a", "b", "c", "a", "b"}
	for i, rule := range []string{r1.ID, r2.ID, r1.ID, r2.ID, r1.ID} {
		h, sev := hosts[i], "warn"
		if rule == r2.ID {
			sev = "critical"
		}
		state := "open"
		var resolved any
		if i == 0 {
			state, resolved = "resolved", time.Now()
		}
		mustExec(t, `INSERT INTO alerts (rule_id, host_id, rule_name, rule_kind, severity, state, opened_at, resolved_at, resolve_reason)
			VALUES ($1, $2, 'x', 'metric', $3, $4, now() - make_interval(mins => $5), $6, CASE WHEN $4 = 'resolved' THEN 'manual' END)`,
			rule, hostID(t, h), sev, state, min(i, 3)*10, resolved)
	}

	page, err := s.ListAlerts(ctx, AllScope(), AlertFilter{Limit: 2})
	if err != nil || len(page.Items) != 2 || page.Total != 5 || page.NextCursor == nil {
		t.Fatalf("pagina 1 got %+v, %v", page, err)
	}
	seen := map[string]bool{}
	for {
		for _, a := range page.Items {
			if seen[a.ID] {
				t.Fatalf("alerta repetida entre paginas: %s", a.ID)
			}
			seen[a.ID] = true
		}
		if page.NextCursor == nil {
			break
		}
		page, err = s.ListAlerts(ctx, AllScope(), AlertFilter{Limit: 2, After: page.NextCursor})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 5 {
		t.Errorf("paginando se vieron %d, want 5", len(seen))
	}

	if p, _ := s.ListAlerts(ctx, AllScope(), AlertFilter{States: []string{"open"}}); p.Total != 4 {
		t.Errorf("state=open total got %d, want 4", p.Total)
	}
	if p, _ := s.ListAlerts(ctx, AllScope(), AlertFilter{Severities: []string{"critical"}}); p.Total != 2 {
		t.Errorf("severity=critical total got %d, want 2", p.Total)
	}
	if p, _ := s.ListAlerts(ctx, AllScope(), AlertFilter{HostIDs: []string{hostID(t, "a")}, RuleIDs: []string{r1.ID}}); p.Total != 1 {
		t.Errorf("host+rule total got %d, want 1", p.Total)
	}
	if p, _ := s.ListAlerts(ctx, AllScope(), AlertFilter{States: []string{"open", "resolved"}, Severities: []string{"warn"}}); p.Total != 3 {
		t.Errorf("state OR + severity total got %d, want 3", p.Total)
	}

	sum, err := s.AlertSummary(ctx, AllScope())
	if err != nil || sum.Active != 4 || sum.BySeverity["critical"].Open != 2 || sum.BySeverity["warn"].Open != 2 || sum.BySeverity["info"].Open != 0 {
		t.Errorf("resumen got %+v, %v", sum, err)
	}
}

func TestAlertByIDIncludesDeliveriesAndHostEvents(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedHost(t, "srv", "up", ago(time.Second))
	rule := mkRule(t, s, metricRule("cpu", "cpu_pct", "gt", 90, 0))
	mkChannel(t, "hook", "info", rule.ID)
	setLatest(t, "srv", 95)
	evalOnce(t, s)
	mustExec(t, `INSERT INTO host_events (host_id, from_status, to_status, source) VALUES ($1, 'up', 'offline', 'detector')`, hostID(t, "srv"))
	var id string
	if err := testPool.QueryRow(ctx, `SELECT id FROM alerts`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	d, err := s.AlertByID(ctx, AllScope(), id)
	if err != nil || d.Alert.Hostname != "srv" || len(d.Deliveries) != 1 || d.Deliveries[0].ChannelName != "hook" || len(d.HostEvents) != 1 {
		t.Fatalf("detalle got %+v, %v", d, err)
	}
	if _, err := s.AlertByID(ctx, AllScope(), "00000000-0000-0000-0000-0000000000aa"); !errors.Is(err, ErrNotFound) {
		t.Errorf("inexistente got %v", err)
	}
}

func TestAlertRuleCRUDAndConstraints(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	g := mkGroup(t, s, "prod", false, nil)

	r := mkRule(t, s, metricRule("CPU", "cpu_pct", "gt", 90, 60))
	if _, err := s.CreateAlertRule(ctx, metricRule("cpu", "cpu_pct", "gt", 90, 60), ""); !errors.Is(err, ErrRuleExists) {
		t.Errorf("nombre repetido got %v, want ErrRuleExists", err)
	}
	bad := metricRule("g", "cpu_pct", "gt", 90, 60)
	bad.Scope = AlertScope{Type: ScopeGroup, GroupID: sptr("00000000-0000-0000-0000-0000000000aa")}
	if _, err := s.CreateAlertRule(ctx, bad, ""); !errors.Is(err, ErrGroupNotFound) {
		t.Errorf("grupo inexistente got %v, want ErrGroupNotFound", err)
	}

	// Los CHECK son la ultima defensa: umbral fuera de rango, histeresis invertida, kind incoherente.
	for name, mut := range map[string]func(*AlertRule){
		"umbral > 100":          func(x *AlertRule) { x.Threshold = ptr(101) },
		"clear > threshold(gt)": func(x *AlertRule) { x.ClearThreshold = ptr(95) },
		"metric sin metric":     func(x *AlertRule) { x.Metric = nil },
		"duracion negativa":     func(x *AlertRule) { x.DurationSeconds = -1 },
		"alcance incoherente":   func(x *AlertRule) { x.Scope = AlertScope{Type: ScopeGroup} },
	} {
		x := metricRule("x-"+name, "cpu_pct", "gt", 90, 60)
		mut(&x)
		if _, err := s.CreateAlertRule(ctx, x, ""); err == nil {
			t.Errorf("%s: la BD debio rechazar la regla", name)
		}
	}

	up, err := s.UpdateAlertRule(ctx, r.ID, func(x *AlertRule) error {
		x.Threshold = ptr(80)
		x.Enabled = false
		x.Scope = AlertScope{Type: ScopeGroup, GroupID: &g.ID}
		return nil
	})
	if err != nil || *up.Threshold != 80 || up.Enabled || up.GroupName == nil || *up.GroupName != "prod" {
		t.Fatalf("update got %+v, %v", up, err)
	}
	sentinel := errors.New("mutate")
	if _, err := s.UpdateAlertRule(ctx, r.ID, func(*AlertRule) error { return sentinel }); !errors.Is(err, sentinel) {
		t.Errorf("error de mutate got %v", err)
	}
	if _, err := s.UpdateAlertRule(ctx, "00000000-0000-0000-0000-0000000000aa", func(*AlertRule) error { return nil }); !errors.Is(err, ErrNotFound) {
		t.Errorf("update inexistente got %v", err)
	}
	if name, err := s.DeleteAlertRule(ctx, r.ID); err != nil || name != "CPU" {
		t.Errorf("delete got %q, %v", name, err)
	}
	if _, err := s.DeleteAlertRule(ctx, r.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("segundo delete got %v", err)
	}
	// Borrar el grupo elimina en cascada las reglas de su alcance (sin dejar reglas huerfanas).
	mkRule(t, s, func() AlertRule {
		x := metricRule("scoped", "cpu_pct", "gt", 90, 60)
		x.Scope = AlertScope{Type: ScopeGroup, GroupID: &g.ID}
		return x
	}())
	if err := s.DeleteGroup(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	if n := count(t, `SELECT count(*) FROM alert_rules WHERE name = 'scoped'`); n != 0 {
		t.Error("la regla del grupo borrado debe caer en cascada")
	}
}

func TestAlertRulesCap(t *testing.T) {
	s := newTestStore(t)
	mustExec(t, `INSERT INTO alert_rules (name, kind, duration_seconds, severity)
		SELECT 'r' || g, 'host_status', 0, 'info' FROM generate_series(1, $1::int) g`, MaxAlertRules)
	_, err := s.CreateAlertRule(context.Background(), metricRule("extra", "cpu_pct", "gt", 90, 0), "")
	if !errors.Is(err, ErrTooManyRules) {
		t.Fatalf("got %v, want ErrTooManyRules", err)
	}
}

func TestMaintenanceCRUDAndCaps(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	uid := mkUser(t, s, "adm", "admin")
	now := time.Now()
	w := MaintenanceWindow{Name: "parche", StartsAt: now.Add(-time.Minute), EndsAt: now.Add(time.Hour), Scope: AlertScope{Type: ScopeAll}}

	got, err := s.CreateMaintenance(ctx, w, uid, "adm")
	if err != nil || got.Status != "active" || got.CreatedByName == nil || *got.CreatedByName != "adm" {
		t.Fatalf("create got %+v, %v", got, err)
	}
	// CHECK: `all` mas de 24 h y mas de 31 dias los rechaza la BD.
	long := w
	long.EndsAt = now.Add(25 * time.Hour)
	if _, err := s.CreateMaintenance(ctx, long, uid, "adm"); err == nil {
		t.Error("all > 24 h debe rechazarse")
	}
	g := mkGroup(t, s, "dev", false, nil)
	long.Scope = AlertScope{Type: ScopeGroup, GroupID: &g.ID}
	if _, err := s.CreateMaintenance(ctx, long, uid, "adm"); err != nil {
		t.Errorf("grupo 25 h debe permitirse: %v", err)
	}
	long.EndsAt = now.Add(32 * 24 * time.Hour)
	if _, err := s.CreateMaintenance(ctx, long, uid, "adm"); err == nil {
		t.Error("> 31 dias debe rechazarse")
	}
	bad := w
	bad.Scope = AlertScope{Type: ScopeGroup, GroupID: sptr("00000000-0000-0000-0000-0000000000aa")}
	if _, err := s.CreateMaintenance(ctx, bad, uid, "adm"); !errors.Is(err, ErrGroupNotFound) {
		t.Errorf("grupo inexistente got %v", err)
	}

	up, err := s.UpdateMaintenance(ctx, got.ID, func(x *MaintenanceWindow) error { x.Name = "otro"; return nil })
	if err != nil || up.Name != "otro" {
		t.Fatalf("update got %+v, %v", up, err)
	}
	list, err := s.ListMaintenance(ctx, AllScope())
	if err != nil || len(list) != 2 {
		t.Fatalf("list got %d, %v", len(list), err)
	}
	if name, err := s.DeleteMaintenance(ctx, got.ID); err != nil || name != "otro" {
		t.Errorf("delete got %q, %v", name, err)
	}
	if _, err := s.DeleteMaintenance(ctx, got.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("segundo delete got %v", err)
	}

	// Tope de ventanas activas o programadas.
	mustExec(t, `INSERT INTO maintenance_windows (name, starts_at, ends_at, scope_type)
		SELECT 'w' || g, now(), now() + interval '1 hour', 'all' FROM generate_series(1, $1::int) g`, MaxActiveMaintenance)
	if _, err := s.CreateMaintenance(ctx, w, uid, "adm"); !errors.Is(err, ErrTooManyWindows) {
		t.Errorf("got %v, want ErrTooManyWindows", err)
	}
}

func TestHostEventsFromEveryWriter(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	events := func(name string) []string {
		rows, err := testPool.Query(ctx,
			`SELECT e.from_status || '>' || e.to_status || ':' || e.source FROM host_events e
			   JOIN hosts h ON h.id = e.host_id WHERE h.hostname = $1 ORDER BY e.id`, name)
		if err != nil {
			t.Fatal(err)
		}
		out, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	want := func(name string, exp ...string) {
		t.Helper()
		got := events(name)
		if len(got) != len(exp) {
			t.Fatalf("%s eventos got %v, want %v", name, got, exp)
		}
		for i := range exp {
			if got[i] != exp[i] {
				t.Fatalf("%s eventos got %v, want %v", name, got, exp)
			}
		}
	}

	// Detector: up -> offline (sin grupo) y up -> down (always_on).
	g := mkGroup(t, s, "srv", true, nil)
	seedHost(t, "lap", "up", ago(10*time.Minute))
	seedHost(t, "srv", "up", ago(10*time.Minute))
	setGroupOf(t, "srv", g.ID)
	if _, err := s.MarkStaleHosts(ctx, 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	want("lap", "up>offline:detector")
	want("srv", "up>down:detector")
	// Un segundo ciclo no repite nada.
	if _, err := s.MarkStaleHosts(ctx, 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	want("lap", "up>offline:detector")

	// Cambio de always_on del grupo: down -> offline (group_change).
	off := false
	if _, err := s.UpdateGroup(ctx, g.ID, GroupChange{AlwaysOn: &off}); err != nil {
		t.Fatal(err)
	}
	want("srv", "up>down:detector", "down>offline:group_change")
	// Asignar host offline a un grupo always_on: offline -> down.
	if _, err := s.SetHostGroup(ctx, hostID(t, "lap"), &g.ID); err != nil {
		t.Fatal(err)
	}
	want("lap", "up>offline:detector") // el grupo ya no es always_on: sin cambio
	on := true
	if _, err := s.UpdateGroup(ctx, g.ID, GroupChange{AlwaysOn: &on}); err != nil {
		t.Fatal(err)
	}
	want("lap", "up>offline:detector", "offline>down:group_change")

	// Ingest: down -> up.
	if _, err := s.IngestMetrics(ctx, hostID(t, "lap"), Sample{Timestamp: time.Now(), MemTotalMB: 1}); err != nil {
		t.Fatal(err)
	}
	want("lap", "up>offline:detector", "offline>down:group_change", "down>up:ingest")
	// Un ingest de un host up no genera evento.
	if _, err := s.IngestMetrics(ctx, hostID(t, "lap"), Sample{Timestamp: time.Now().Add(time.Second), MemTotalMB: 1}); err != nil {
		t.Fatal(err)
	}
	want("lap", "up>offline:detector", "offline>down:group_change", "down>up:ingest")

	// Revocar: up -> disabled (admin), idempotente; el bulk disable tambien registra.
	if err := s.RevokeHost(ctx, hostID(t, "lap")); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeHost(ctx, hostID(t, "lap")); err != nil {
		t.Fatal(err)
	}
	want("lap", "up>offline:detector", "offline>down:group_change", "down>up:ingest", "up>disabled:admin")
	if err := s.RevokeHost(ctx, "00000000-0000-0000-0000-0000000000aa"); !errors.Is(err, ErrNotFound) {
		t.Errorf("revocar inexistente got %v", err)
	}
	if n, err := s.BulkHosts(ctx, Bulk{IDs: []string{hostID(t, "srv"), hostID(t, "lap")}, Action: BulkDisable}); err != nil || n != 1 {
		t.Fatalf("bulk disable got %d, %v; want 1 (lap ya estaba deshabilitado)", n, err)
	}
	want("srv", "up>down:detector", "down>offline:group_change", "offline>down:group_change", "down>disabled:admin")

	// Listado paginado por cursor.
	page, err := s.ListHostEvents(ctx, AllScope(), hostID(t, "lap"), nil, 2)
	if err != nil || len(page.Items) != 2 || page.NextCursor == nil || page.Items[0].ToStatus != "disabled" {
		t.Fatalf("pagina 1 got %+v, %v", page, err)
	}
	page, err = s.ListHostEvents(ctx, AllScope(), hostID(t, "lap"), page.NextCursor, 2)
	if err != nil || len(page.Items) != 2 || page.NextCursor != nil {
		t.Fatalf("pagina 2 got %+v, %v", page, err)
	}
	if _, err := s.ListHostEvents(ctx, AllScope(), "00000000-0000-0000-0000-0000000000aa", nil, 10); !errors.Is(err, ErrNotFound) {
		t.Errorf("host inexistente got %v", err)
	}
}

func TestPurgeAlertData(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedHost(t, "srv", "up", ago(time.Second))
	h := hostID(t, "srv")
	ch := mkChannel(t, "hook", "info", "")

	mustExec(t, `INSERT INTO host_events (host_id, from_status, to_status, at, source) VALUES
		($1, 'up', 'down', now() - interval '200 days', 'detector'), ($1, 'down', 'up', now() - interval '1 day', 'ingest')`, h)
	if n, err := s.PurgeHostEvents(ctx, time.Now().Add(-180*24*time.Hour), 1000); err != nil || n != 1 {
		t.Errorf("purge host_events got %d, %v; want 1", n, err)
	}

	mustExec(t, `INSERT INTO alerts (id, host_id, rule_name, rule_kind, severity, state, opened_at, resolved_at, resolve_reason) VALUES
		('00000000-0000-0000-0000-000000000001', $1, 'x', 'metric', 'warn', 'resolved', now() - interval '400 days', now() - interval '399 days', 'manual'),
		('00000000-0000-0000-0000-000000000002', $1, 'x', 'metric', 'warn', 'resolved', now() - interval '2 days', now() - interval '1 day', 'manual'),
		('00000000-0000-0000-0000-000000000003', $1, 'x', 'metric', 'warn', 'open', now() - interval '500 days', NULL, NULL)`, h)
	mustExec(t, `INSERT INTO notification_deliveries (alert_id, channel_id, event, status, created_at) VALUES
		('00000000-0000-0000-0000-000000000002', $1, 'opened', 'sent', now() - interval '40 days'),
		('00000000-0000-0000-0000-000000000002', $1, 'resolved', 'pending', now() - interval '40 days'),
		('00000000-0000-0000-0000-000000000002', $1, 'reminder', 'sent', now() - interval '1 day')`, ch)
	if n, err := s.PurgeDeliveries(ctx, time.Now().Add(-30*24*time.Hour), 1000); err != nil || n != 1 {
		t.Errorf("purge entregas got %d, %v; want 1 (la pending vieja es trabajo vivo)", n, err)
	}
	if n, err := s.PurgeResolvedAlerts(ctx, time.Now().Add(-365*24*time.Hour), 1000); err != nil || n != 1 {
		t.Errorf("purge alertas got %d, %v; want 1 (la abierta vieja se conserva)", n, err)
	}
	if n := count(t, `SELECT count(*) FROM alerts`); n != 2 {
		t.Errorf("alertas restantes got %d, want 2", n)
	}

	mustExec(t, `INSERT INTO maintenance_windows (name, starts_at, ends_at, scope_type) VALUES
		('vieja', now() - interval '400 days', now() - interval '399 days', 'all'), ('nueva', now(), now() + interval '1 hour', 'all')`)
	if n, err := s.PurgeMaintenanceWindows(ctx, time.Now().Add(-365*24*time.Hour), 1000); err != nil || n != 1 {
		t.Errorf("purge ventanas got %d, %v; want 1", n, err)
	}
}

func TestMigrateDownToThirteenAndUpAgain(t *testing.T) {
	if testPool == nil {
		t.Skip("ARGOS_TEST_DATABASE_URL no definida")
	}
	ctx := context.Background()
	if err := resetSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if got := count(t, `SELECT count(*) FROM alert_rules WHERE NOT enabled`); got != 5 {
		t.Errorf("reglas de arranque desactivadas got %d, want 5", got)
	}
	if got := count(t, `SELECT count(*) FROM alert_rules WHERE enabled`); got != 0 {
		t.Errorf("reglas de arranque activas got %d, want 0", got)
	}
	provider, err := newProvider(testPool)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.DownTo(ctx, 13); err != nil {
		t.Fatalf("down a 13: %v", err)
	}
	if got := count(t, `SELECT count(*) FROM pg_tables WHERE schemaname = 'public' AND tablename IN ('alerts', 'host_events', 'alert_rules')`); got != 0 {
		t.Fatalf("las tablas de alertas siguen tras down a 13")
	}
	if err := Migrate(ctx, testPool); err != nil {
		t.Fatalf("up tras down: %v", err)
	}
}
