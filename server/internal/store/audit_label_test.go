package store

import (
	"context"
	"testing"
)

// Un objeto borrado conserva su nombre en la auditoria: el evento de borrado (sin nombre propio) toma
// el ultimo guardado para ese objetivo, y la lista lo muestra con target_exists=false.
func TestAuditKeepsTargetNameAfterDelete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	const id = "4f8a1c2e-0000-4000-8000-000000000001"
	if err := s.InsertAudit(ctx, AuditEntry{Event: "zt_created", Outcome: OutcomeOK, TargetType: "channel", TargetID: id,
		Detail: []byte(`{"name":"canal-borrado"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertAudit(ctx, AuditEntry{Event: "zt_deleted", Outcome: OutcomeOK, TargetType: "channel", TargetID: id}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertAudit(ctx, AuditEntry{Event: "zt_purged", Outcome: OutcomeOK, TargetType: "host", TargetID: id, TargetLabel: "srv-x"}); err != nil {
		t.Fatal(err)
	}
	page, err := s.ListAudit(ctx, AuditFilter{Events: []string{"zt_created", "zt_deleted", "zt_purged"}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"zt_created": "canal-borrado", "zt_deleted": "canal-borrado", "zt_purged": "srv-x"}
	for _, r := range page.Items {
		if r.TargetName == nil || *r.TargetName != want[r.Event] || r.TargetExists {
			t.Errorf("%s: target_name=%v exists=%v, want %q y false", r.Event, r.TargetName, r.TargetExists, want[r.Event])
		}
	}
	// La busqueda tambien encuentra por el nombre guardado.
	if page, err = s.ListAudit(ctx, AuditFilter{Events: []string{"zt_deleted"}, Query: "BORRADO", Limit: 10}); err != nil || page.Total != 1 {
		t.Errorf("q por nombre guardado: total=%d err=%v, want 1", page.Total, err)
	}
}
