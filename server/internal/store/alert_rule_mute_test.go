package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestMuteRuleSuppressesOpenedButAlertStillOpens (B19-B30, seccion 2): con muted_until en el futuro, la alerta se
// abre igual (fila en alerts) pero su entrega `opened` queda suppressed, no pending. El silencio no toca la
// evaluacion, solo el encolado de notificaciones nuevas.
func TestMuteRuleSuppressesOpenedButAlertStillOpens(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedHost(t, "srv", "up", ago(time.Second))
	rule := mkRule(t, s, metricRule("cpu", "cpu_pct", "gt", 90, 0))
	mkChannel(t, "hook", "warn", rule.ID)

	until := time.Now().Add(time.Hour)
	if err := s.MuteRule(ctx, rule.ID, &until); err != nil {
		t.Fatalf("MuteRule: %v", err)
	}

	setLatest(t, "srv", 95)
	if r := evalOnce(t, s); r.Opened != 1 {
		t.Fatalf("Opened got %d, want 1", r.Opened)
	}
	if a := oneAlert(t); a.State != "open" || a.Suppressed {
		t.Fatalf("alerta got %+v, want open y no suppressed (el mute no es mantenimiento)", a)
	}
	if got := deliveries(t, "opened", "suppressed"); got != 1 {
		t.Errorf("entregas opened suppressed got %d, want 1", got)
	}
	if got := deliveries(t, "opened", "pending"); got != 0 {
		t.Errorf("entregas opened pending got %d, want 0 (regla silenciada)", got)
	}
}

// TestMuteRuleDoesNotSuppressResolved: una alerta abierta antes del silencio, que se cierra con la regla ya
// silenciada, no queda suppressed por el mute: la entrega `resolved` sigue el flujo normal (si se aviso la
// apertura, se avisa el cierre).
func TestMuteRuleDoesNotSuppressResolved(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedHost(t, "srv", "up", ago(time.Second))
	rule := mkRule(t, s, metricRule("cpu", "cpu_pct", "gt", 90, 0))
	mkChannel(t, "hook", "warn", rule.ID)

	setLatest(t, "srv", 95)
	evalOnce(t, s)
	if got := deliveries(t, "opened", "pending"); got != 1 {
		t.Fatalf("entregas opened pending got %d, want 1 (sin silencio aun)", got)
	}

	until := time.Now().Add(time.Hour)
	if err := s.MuteRule(ctx, rule.ID, &until); err != nil {
		t.Fatalf("MuteRule: %v", err)
	}

	// resolve_seconds es 0 (default de metricRule): se resuelve en el mismo ciclo en que queda sano.
	setLatest(t, "srv", 50)
	if r := evalOnce(t, s); r.Resolved != 1 {
		t.Fatalf("Resolved got %d, want 1", r.Resolved)
	}
	if got := deliveries(t, "resolved", "pending"); got != 1 {
		t.Errorf("entregas resolved pending got %d, want 1 (resolved no se silencia)", got)
	}
	if got := deliveries(t, "resolved", "suppressed"); got != 0 {
		t.Errorf("entregas resolved suppressed got %d, want 0", got)
	}
}

// TestMuteRuleNotFound: silenciar una regla inexistente devuelve ErrNotFound (mismo patron que UpdateAlertRule).
func TestMuteRuleNotFound(t *testing.T) {
	s := newTestStore(t)
	until := time.Now().Add(time.Hour)
	if err := s.MuteRule(context.Background(), "00000000-0000-0000-0000-000000000000", &until); !errors.Is(err, ErrNotFound) {
		t.Fatalf("MuteRule got %v, want ErrNotFound", err)
	}
}
