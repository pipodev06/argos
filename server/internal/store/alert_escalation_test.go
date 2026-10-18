package store

import (
	"testing"
)

// escalationSetup crea host, regla con plazo (nil = sin plazo), un canal normal y uno de escalamiento, ejecuta
// beforeSQL si no es vacio y abre la alerta.
func escalationSetup(t *testing.T, s *Store, severity string, after *int, beforeSQL string) (rule AlertRule, normalCh, escCh string) {
	t.Helper()
	seedHost(t, "srv", "up", ago(0))
	r := metricRule("cpu", "cpu_pct", "gt", 90, 0)
	r.Severity = severity
	r.EscalateAfterSeconds = after
	rule = mkRule(t, s, r)
	normalCh = mkChannel(t, "normal", "info", rule.ID)
	escCh = mkChannel(t, "escala", "info", "")
	if err := s.SetRuleEscalationChannels(t.Context(), rule.ID, []string{escCh}); err != nil {
		t.Fatalf("SetRuleEscalationChannels: %v", err)
	}
	if beforeSQL != "" {
		mustExec(t, beforeSQL)
	}
	setLatest(t, "srv", 95)
	if res := evalOnce(t, s); res.Opened != 1 {
		t.Fatalf("Opened got %d, want 1", res.Opened)
	}
	return rule, normalCh, escCh
}

func ageAlert(t *testing.T) {
	t.Helper()
	mustExec(t, `UPDATE alerts SET opened_at = now() - interval '10 minutes'`)
}

func escalationDeliveries(t *testing.T, channelID string) int {
	t.Helper()
	return count(t, `SELECT count(*) FROM notification_deliveries WHERE event = 'escalation' AND channel_id = $1`, channelID)
}

func alertSeverity(t *testing.T) (severity string, escalated bool) {
	t.Helper()
	if err := testPool.QueryRow(t.Context(), `SELECT severity, escalated_at IS NOT NULL FROM alerts`).Scan(&severity, &escalated); err != nil {
		t.Fatalf("leyendo alerta: %v", err)
	}
	return severity, escalated
}

func TestEscalationRaisesSeverityAndNotifiesOnlyEscalationChannels(t *testing.T) {
	s := newTestStore(t)
	_, normalCh, escCh := escalationSetup(t, s, "warn", secs(300), "")

	// Plazo aun no vencido: no escala.
	if res := evalOnce(t, s); res.Escalated != 0 {
		t.Fatalf("Escalated antes del plazo got %d, want 0", res.Escalated)
	}
	ageAlert(t)
	if res := evalOnce(t, s); res.Escalated != 1 {
		t.Fatalf("Escalated got %d, want 1", res.Escalated)
	}
	if sev, esc := alertSeverity(t); sev != "critical" || !esc {
		t.Fatalf("alerta got severity=%s escalated=%v, want critical/true", sev, esc)
	}
	if got := escalationDeliveries(t, escCh); got != 1 {
		t.Errorf("escalation al canal de escalamiento got %d, want 1", got)
	}
	if got := escalationDeliveries(t, normalCh); got != 0 {
		t.Errorf("escalation al canal normal got %d, want 0", got)
	}

	// Una sola vez: otro ciclo no vuelve a subir ni a encolar.
	if res := evalOnce(t, s); res.Escalated != 0 {
		t.Errorf("segundo ciclo Escalated got %d, want 0", res.Escalated)
	}
	if got := escalationDeliveries(t, escCh); got != 1 {
		t.Errorf("escalation tras segundo ciclo got %d, want 1", got)
	}
}

func TestEscalationInfoGoesToWarn(t *testing.T) {
	s := newTestStore(t)
	escalationSetup(t, s, "info", secs(300), "")
	ageAlert(t)
	evalOnce(t, s)
	if sev, _ := alertSeverity(t); sev != "warn" {
		t.Errorf("severity got %s, want warn", sev)
	}
}

func TestEscalationCriticalStaysCriticalButNotifies(t *testing.T) {
	s := newTestStore(t)
	_, _, escCh := escalationSetup(t, s, "critical", secs(300), "")
	ageAlert(t)
	if res := evalOnce(t, s); res.Escalated != 1 {
		t.Fatalf("Escalated got %d, want 1", res.Escalated)
	}
	if sev, esc := alertSeverity(t); sev != "critical" || !esc {
		t.Errorf("alerta got severity=%s escalated=%v, want critical/true", sev, esc)
	}
	if got := escalationDeliveries(t, escCh); got != 1 {
		t.Errorf("escalation got %d, want 1", got)
	}
}

func TestEscalationRespectsChannelMinSeverityWithRaisedSeverity(t *testing.T) {
	s := newTestStore(t)
	rule, _, _ := escalationSetup(t, s, "warn", secs(300), "")
	critOnly := mkChannel(t, "solo-critical", "critical", "")
	if err := s.SetRuleEscalationChannels(t.Context(), rule.ID, []string{critOnly}); err != nil {
		t.Fatal(err)
	}
	ageAlert(t)
	evalOnce(t, s)
	if got := escalationDeliveries(t, critOnly); got != 1 {
		t.Errorf("canal min_severity critical got %d escalation, want 1 (la severidad ya subida es la que cuenta)", got)
	}
}

func TestEscalationSkipsIneligibleAlerts(t *testing.T) {
	tests := []struct {
		name   string
		after  *int
		before string // SQL previo a abrir la alerta
		mark   string // SQL que vuelve inelegible la alerta
	}{
		{"reconocida", secs(300), "", `UPDATE alerts SET state = 'acknowledged', acknowledged_at = now(), acknowledged_by_name = 'u'`},
		{"en mantenimiento", secs(300), `INSERT INTO maintenance_windows (name, starts_at, ends_at, scope_type) VALUES ('m', now() - interval '1 hour', now() + interval '1 hour', 'all')`, `SELECT 1`},
		{"intermitente", secs(300), "", `UPDATE alerts SET flapping = true, flap_held = true`},
		{"regla sin plazo", nil, "", `SELECT 1`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			_, _, escCh := escalationSetup(t, s, "warn", tc.after, tc.before)
			mustExec(t, tc.mark)
			ageAlert(t)
			if res := evalOnce(t, s); res.Escalated != 0 {
				t.Fatalf("Escalated got %d, want 0", res.Escalated)
			}
			if sev, esc := alertSeverity(t); sev != "warn" || esc {
				t.Errorf("alerta got severity=%s escalated=%v, want warn/false", sev, esc)
			}
			if got := escalationDeliveries(t, escCh); got != 0 {
				t.Errorf("escalation got %d, want 0", got)
			}
		})
	}
}

func TestEscalationWithoutChannelsOnlyRaisesSeverity(t *testing.T) {
	s := newTestStore(t)
	rule, _, _ := escalationSetup(t, s, "warn", secs(300), "")
	if err := s.SetRuleEscalationChannels(t.Context(), rule.ID, nil); err != nil {
		t.Fatal(err)
	}
	ageAlert(t)
	if res := evalOnce(t, s); res.Escalated != 1 {
		t.Fatalf("Escalated got %d, want 1", res.Escalated)
	}
	if sev, _ := alertSeverity(t); sev != "critical" {
		t.Errorf("severity got %s, want critical", sev)
	}
	if got := count(t, `SELECT count(*) FROM notification_deliveries WHERE event = 'escalation'`); got != 0 {
		t.Errorf("entregas escalation got %d, want 0", got)
	}
}

func TestResolvedOfEscalatedAlertReachesEscalationChannel(t *testing.T) {
	s := newTestStore(t)
	_, normalCh, escCh := escalationSetup(t, s, "warn", secs(300), "")
	ageAlert(t)
	evalOnce(t, s)
	setLatest(t, "srv", 50)
	if res := evalOnce(t, s); res.Resolved != 1 {
		t.Fatalf("Resolved got %d, want 1", res.Resolved)
	}
	for name, ch := range map[string]string{"escalamiento": escCh, "normal": normalCh} {
		if got := count(t, `SELECT count(*) FROM notification_deliveries WHERE event = 'resolved' AND channel_id = $1`, ch); got != 1 {
			t.Errorf("resolved al canal %s got %d, want 1", name, got)
		}
	}
}

func secs(n int) *int { return &n }
