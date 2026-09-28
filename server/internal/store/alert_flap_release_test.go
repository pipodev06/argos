package store

import (
	"context"
	"testing"
	"time"
)

// flapHeldSetup deja una alerta que abrio como intermitente (3.a apertura de la regla en la ventana) y retenida.
func flapHeldSetup(t *testing.T, s *Store) AlertRule {
	t.Helper()
	seedHost(t, "srv", "up", ago(time.Second))
	rule := mkRule(t, s, metricRule("cpu", "cpu_pct", "gt", 90, 0))
	mkChannel(t, "hook", "warn", rule.ID)
	for i := 1; i <= FlapThreshold-1; i++ {
		flapCycle(t, s, "srv")
	}
	setLatest(t, "srv", 95)
	if r := evalOnce(t, s); r.Opened != 1 {
		t.Fatalf("Opened got %d, want 1", r.Opened)
	}
	if got := count(t, `SELECT count(*) FROM alerts WHERE flapping AND flap_held AND state = 'open'`); got != 1 {
		t.Fatal("la 3.a apertura debe quedar flapping y flap_held")
	}
	return rule
}

// flapDeliveries cuenta las entregas de la alerta intermitente (las previas, no intermitentes, tienen las suyas).
func flapDeliveries(t *testing.T, event, status string) int {
	t.Helper()
	return count(t, `SELECT count(*) FROM notification_deliveries d JOIN alerts a ON a.id = d.alert_id
	                  WHERE a.flapping AND d.event = $1 AND d.status = $2`, event, status)
}

// stabilize simula que la alerta lleva abierta mas que la ventana de flapping.
func stabilize(t *testing.T) {
	t.Helper()
	mustExec(t, `UPDATE alerts SET opened_at = now() - make_interval(secs => $1) WHERE state = 'open'`, float64(FlapWindow+60))
}

func TestFlapHeldReleasedWhenStillOpenAfterWindow(t *testing.T) {
	s := newTestStore(t)
	flapHeldSetup(t, s)
	if r := evalOnce(t, s); r.FlapReleased != 0 {
		t.Fatalf("FlapReleased got %d, want 0 (aun dentro de la ventana)", r.FlapReleased)
	}
	stabilize(t)
	if r := evalOnce(t, s); r.FlapReleased != 1 {
		t.Fatalf("FlapReleased got %d, want 1", r.FlapReleased)
	}
	if flapDeliveries(t, "opened", "pending") != 1 || flapDeliveries(t, "opened", "suppressed") != 0 {
		t.Fatal("una intermitente que sigue abierta debe avisar: opened pasa a pending")
	}
	if got := count(t, `SELECT count(*) FROM alerts WHERE flapping AND NOT flap_held`); got != 1 {
		t.Error("flapping se conserva como historial; flap_held se limpia")
	}
	// Ya con el opened anunciado, al resolverse se avisa el cierre.
	setLatest(t, "srv", 50)
	evalOnce(t, s)
	if got := flapDeliveries(t, "resolved", "pending"); got != 1 {
		t.Errorf("entregas resolved pending got %d, want 1", got)
	}
}

func TestFlapHeldResolvedInsideWindowStaysSilent(t *testing.T) {
	s := newTestStore(t)
	flapHeldSetup(t, s)
	setLatest(t, "srv", 50)
	evalOnce(t, s) // resuelve la alerta dentro de la ventana
	evalOnce(t, s)
	if got := count(t, `SELECT count(*) FROM alerts WHERE flap_held`); got != 0 {
		t.Errorf("la marca flap_held debe limpiarse al resolverse, got %d", got)
	}
	if flapDeliveries(t, "opened", "pending") != 0 || flapDeliveries(t, "resolved", "pending") != 0 {
		t.Fatal("una intermitente que se resuelve dentro de la ventana no avisa ni abre ni cierra")
	}
}

func TestFlapHeldNotReleasedByMaintenanceEnding(t *testing.T) {
	s := newTestStore(t)
	flapHeldSetup(t, s)
	// Fin de mantenimiento: la alerta estaba `suppressed`; al liberarse no debe adelantar el aviso de la intermitente.
	mustExec(t, `UPDATE alerts SET suppressed = true WHERE state = 'open'`)
	evalOnce(t, s)
	if flapDeliveries(t, "opened", "pending") != 0 {
		t.Fatal("el fin de mantenimiento no debe liberar el opened de una alerta retenida por flapping")
	}
}

func TestFlapReleaseWithMutedRuleHandsOverToMute(t *testing.T) {
	s := newTestStore(t)
	rule := flapHeldSetup(t, s)
	until := time.Now().Add(time.Hour)
	if err := s.MuteRule(context.Background(), rule.ID, &until); err != nil {
		t.Fatalf("MuteRule: %v", err)
	}
	stabilize(t)
	evalOnce(t, s)
	if flapDeliveries(t, "opened", "pending") != 0 || count(t, `SELECT count(*) FROM alerts WHERE mute_held AND NOT flap_held`) != 1 {
		t.Fatal("con la regla silenciada la retencion pasa a mute_held y no avisa aun")
	}
	endMute(t)
	if r := evalOnce(t, s); r.MuteReleased != 1 {
		t.Fatalf("MuteReleased got %d, want 1", r.MuteReleased)
	}
	if flapDeliveries(t, "opened", "pending") != 1 {
		t.Fatal("al terminar el silencio debe avisar")
	}
}
