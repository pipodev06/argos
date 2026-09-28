package store

import (
	"context"
	"testing"
	"time"
)

// muteHeldSetup deja una regla critica silenciada 1 h y una alerta ya abierta bajo ese silencio.
func muteHeldSetup(t *testing.T, s *Store) {
	t.Helper()
	seedHost(t, "srv", "up", ago(time.Second))
	rule := metricRule("cpu", "cpu_pct", "gt", 90, 0)
	rule.Severity = "critical"
	rule = mkRule(t, s, rule)
	mkChannel(t, "hook", "info", rule.ID)
	until := time.Now().Add(time.Hour)
	if err := s.MuteRule(context.Background(), rule.ID, &until); err != nil {
		t.Fatalf("MuteRule: %v", err)
	}
	setLatest(t, "srv", 95)
	if r := evalOnce(t, s); r.Opened != 1 {
		t.Fatalf("Opened got %d, want 1", r.Opened)
	}
}

func endMute(t *testing.T) {
	t.Helper()
	mustExec(t, `UPDATE alert_rules SET muted_until = now() - interval '1 minute'`)
}

func muteHeld(t *testing.T) bool {
	t.Helper()
	return count(t, `SELECT count(*) FROM alerts WHERE mute_held`) == 1
}

func TestMuteReleaseSendsOpenedWhenMuteEnds(t *testing.T) {
	s := newTestStore(t)
	muteHeldSetup(t, s)
	if !muteHeld(t) || deliveries(t, "opened", "suppressed") != 1 {
		t.Fatal("con silencio la alerta debe quedar mute_held y su opened suppressed")
	}
	if r := evalOnce(t, s); r.MuteReleased != 0 {
		t.Fatalf("MuteReleased got %d, want 0 (silencio vigente)", r.MuteReleased)
	}
	endMute(t)
	if r := evalOnce(t, s); r.MuteReleased != 1 {
		t.Fatalf("MuteReleased got %d, want 1", r.MuteReleased)
	}
	if muteHeld(t) || deliveries(t, "opened", "pending") != 1 || deliveries(t, "opened", "suppressed") != 0 {
		t.Fatal("tras el silencio opened debe pasar a pending y limpiarse la marca")
	}
	// Con el opened pendiente el ciclo de vida vuelve a ser normal: al resolverse se avisa.
	setLatest(t, "srv", 50)
	evalOnce(t, s)
	if got := deliveries(t, "resolved", "pending"); got != 1 {
		t.Errorf("entregas resolved pending got %d, want 1", got)
	}
}

func TestMuteReleaseAcknowledgedOnlyClearsMark(t *testing.T) {
	s := newTestStore(t)
	muteHeldSetup(t, s)
	mustExec(t, `UPDATE alerts SET state = 'acknowledged', acknowledged_at = now(), acknowledged_by_name = 'u'`)
	endMute(t)
	evalOnce(t, s)
	if muteHeld(t) {
		t.Error("la marca mute_held debe limpiarse")
	}
	if got := deliveries(t, "opened", "pending"); got != 0 {
		t.Errorf("entregas opened pending got %d, want 0 (reconocida)", got)
	}
}

func TestMuteReleaseWaitsForMaintenanceAndMute(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "srv", "up", ago(time.Second))
	rule := metricRule("cpu", "cpu_pct", "gt", 90, 0)
	rule.Severity = "critical"
	rule = mkRule(t, s, rule)
	mkChannel(t, "hook", "info", rule.ID)
	mustExec(t, `INSERT INTO maintenance_windows (name, starts_at, ends_at, scope_type)
		VALUES ('m', now() - interval '1 hour', now() + interval '1 hour', 'all')`)
	until := time.Now().Add(time.Hour)
	if err := s.MuteRule(context.Background(), rule.ID, &until); err != nil {
		t.Fatalf("MuteRule: %v", err)
	}
	setLatest(t, "srv", 95)
	evalOnce(t, s)
	if got := count(t, `SELECT count(*) FROM alerts WHERE suppressed AND NOT mute_held`); got != 1 {
		t.Fatal("una alerta en mantenimiento tiene su propio ciclo: no debe marcarse mute_held")
	}

	// Alerta abierta antes del mantenimiento actual para cubrir la combinacion: se fuerza mute_held a mano.
	mustExec(t, `UPDATE alerts SET mute_held = true`)
	endMute(t)
	evalOnce(t, s)
	if deliveries(t, "opened", "pending") != 0 {
		t.Fatal("no debe liberarse al terminar el silencio con mantenimiento vigente")
	}
	mustExec(t, `UPDATE alert_rules SET muted_until = now() + interval '1 hour'`)
	mustExec(t, `UPDATE alerts SET mute_held = true`)
	mustExec(t, `DELETE FROM maintenance_windows`)
	evalOnce(t, s)
	if deliveries(t, "opened", "pending") != 0 || !muteHeld(t) {
		t.Fatal("no debe liberarse al terminar el mantenimiento con el silencio vigente")
	}
	endMute(t)
	evalOnce(t, s)
	if deliveries(t, "opened", "pending") != 1 || muteHeld(t) {
		t.Fatal("con ambos terminados debe liberarse")
	}
}

func TestMuteReleaseFlappingNeverHeld(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "srv", "up", ago(time.Second))
	rule := mkRule(t, s, metricRule("cpu", "cpu_pct", "gt", 90, 0))
	mkChannel(t, "hook", "warn", rule.ID)
	for i := 1; i <= FlapThreshold-1; i++ {
		flapCycle(t, s, "srv")
	}
	until := time.Now().Add(time.Hour)
	if err := s.MuteRule(context.Background(), rule.ID, &until); err != nil {
		t.Fatalf("MuteRule: %v", err)
	}
	setLatest(t, "srv", 95)
	evalOnce(t, s)
	if got := count(t, `SELECT count(*) FROM alerts WHERE flapping AND NOT mute_held AND state = 'open'`); got != 1 {
		t.Fatal("una alerta flapping no debe marcarse mute_held")
	}
	endMute(t)
	if r := evalOnce(t, s); r.MuteReleased != 0 {
		t.Fatalf("MuteReleased got %d, want 0", r.MuteReleased)
	}
	if f, st := lastAlertFlap(t, "srv"); !f || st != "suppressed" {
		t.Fatalf("got flapping=%v opened=%s, want true/suppressed", f, st)
	}
}
