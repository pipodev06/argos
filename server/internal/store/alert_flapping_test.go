package store

import (
	"testing"
	"time"
)

// flapCycle abre y resuelve una alerta de la regla sobre el host (resolve_seconds y duration en 0).
func flapCycle(t *testing.T, s *Store, host string) {
	t.Helper()
	setLatest(t, host, 95)
	if r := evalOnce(t, s); r.Opened != 1 {
		t.Fatalf("Opened got %d, want 1", r.Opened)
	}
	setLatest(t, host, 50)
	if r := evalOnce(t, s); r.Resolved != 1 {
		t.Fatalf("Resolved got %d, want 1", r.Resolved)
	}
}

// lastAlertFlap devuelve flapping de la alerta mas reciente del host y el estado de su entrega `opened`.
func lastAlertFlap(t *testing.T, host string) (flapping bool, openedStatus string) {
	t.Helper()
	err := testPool.QueryRow(t.Context(),
		`SELECT a.flapping, d.status FROM alerts a JOIN notification_deliveries d ON d.alert_id = a.id AND d.event = 'opened'
		  WHERE a.host_id = $1 ORDER BY a.opened_at DESC LIMIT 1`, hostID(t, host)).Scan(&flapping, &openedStatus)
	if err != nil {
		t.Fatalf("leyendo ultima alerta de %s: %v", host, err)
	}
	return flapping, openedStatus
}

func TestFlappingThirdOpeningInWindowIsSuppressed(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "srv", "up", ago(time.Second))
	rule := mkRule(t, s, metricRule("cpu", "cpu_pct", "gt", 90, 0))
	mkChannel(t, "hook", "warn", rule.ID)

	for i := 1; i <= FlapThreshold-1; i++ {
		flapCycle(t, s, "srv")
		if f, st := lastAlertFlap(t, "srv"); f || st != "pending" {
			t.Fatalf("apertura %d got flapping=%v opened=%s, want false/pending", i, f, st)
		}
	}
	setLatest(t, "srv", 95)
	evalOnce(t, s)
	if f, st := lastAlertFlap(t, "srv"); !f || st != "suppressed" {
		t.Fatalf("3.a apertura got flapping=%v opened=%s, want true/suppressed", f, st)
	}
	if got := count(t, `SELECT count(*) FROM alerts WHERE flapping AND suppressed`); got != 0 {
		t.Errorf("alertas flapping con suppressed got %d, want 0: flapping no es mantenimiento", got)
	}
	// Al resolverse no se anuncia el cierre de algo que nunca se anuncio.
	setLatest(t, "srv", 50)
	evalOnce(t, s)
	if got := count(t, `SELECT count(*) FROM notification_deliveries d JOIN alerts a ON a.id = d.alert_id
	                     WHERE a.flapping AND d.event = 'resolved'`); got != 0 {
		t.Errorf("entregas resolved de la alerta flapping got %d, want 0", got)
	}
}

func TestFlappingNotTriggeredByOneOrOldPreviousAlerts(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "srv", "up", ago(time.Second))
	rule := mkRule(t, s, metricRule("cpu", "cpu_pct", "gt", 90, 0))
	mkChannel(t, "hook", "warn", rule.ID)

	// Solo 1 previa: la 2.a abre normal.
	flapCycle(t, s, "srv")
	flapCycle(t, s, "srv")
	if f, st := lastAlertFlap(t, "srv"); f || st != "pending" {
		t.Fatalf("2.a apertura got flapping=%v opened=%s, want false/pending", f, st)
	}

	// Previas fuera de la ventana: no cuentan.
	mustExec(t, `UPDATE alerts SET opened_at = now() - interval '2 hours', resolved_at = now() - interval '110 minutes'`)
	setLatest(t, "srv", 95)
	evalOnce(t, s)
	if f, st := lastAlertFlap(t, "srv"); f || st != "pending" {
		t.Fatalf("apertura con previas viejas got flapping=%v opened=%s, want false/pending", f, st)
	}
}

func TestFlappingIgnoresOtherHost(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "a", "up", ago(time.Second))
	seedHost(t, "b", "up", ago(time.Second))
	rule := mkRule(t, s, metricRule("cpu", "cpu_pct", "gt", 90, 0))
	mkChannel(t, "hook", "warn", rule.ID)

	flapCycle(t, s, "a")
	flapCycle(t, s, "a")
	setLatest(t, "b", 95)
	evalOnce(t, s)
	if f, st := lastAlertFlap(t, "b"); f || st != "pending" {
		t.Fatalf("host b got flapping=%v opened=%s, want false/pending (las previas son de a)", f, st)
	}
}
