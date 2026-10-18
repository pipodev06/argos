package store

import (
	"context"
	"testing"
	"time"
)

// El export de hosts corta en maxInventoryExportRows sin error (mismo criterio que el export de grupo) y no mezcla dispositivos.
func TestExportHostsCapAndKinds(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedHost(t, "col", "up", ago(time.Second))
	mkDevice(t, s, "router", "10.0.0.5", "col", nil)
	mustExec(t,
		`INSERT INTO hosts (hostname, agent_token_hash, status)
		   SELECT 'h-' || i, 'hash-' || i, 'pending' FROM generate_series(1, $1) i`, maxInventoryExportRows)

	hosts, err := s.ExportHosts(ctx, AllScope(), HostFilter{})
	if err != nil {
		t.Fatalf("ExportHosts: %v", err)
	}
	if len(hosts) != maxInventoryExportRows {
		t.Errorf("got %d hosts, want exactamente el tope %d (hay %d agentes: corte en silencio)", len(hosts), maxInventoryExportRows, maxInventoryExportRows+1)
	}
	for _, h := range hosts {
		if h.Hostname == "router" {
			t.Fatalf("ExportHosts incluyo un dispositivo")
		}
	}

}
