package store

import (
	"context"
	"testing"
)

// TestExportGroupHostsCap fija el tope maxGroupExportHosts=5000 (B17, tests-pendientes.md): con mas hosts que
// el tope, ExportGroupHosts corta ahi en silencio (sin error ni 422) en vez de devolverlos todos. Insercion
// masiva por SQL (no un host por llamada): es solo para forzar el volumen, no para probar el alta de hosts.
func TestExportGroupHostsCap(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	g := mkGroup(t, s, "grande", false, nil)
	mustExec(t,
		`INSERT INTO hosts (hostname, agent_token_hash, status, group_id)
		   SELECT 'h-' || i, 'hash-' || i, 'pending', $1 FROM generate_series(1, 5001) i`, g.ID)

	hosts, err := s.ExportGroupHosts(ctx, g.ID)
	if err != nil {
		t.Fatalf("ExportGroupHosts: %v", err)
	}
	if len(hosts) != maxGroupExportHosts {
		t.Errorf("got %d hosts, want exactamente el tope %d (corte en silencio, no 422)", len(hosts), maxGroupExportHosts)
	}
}
