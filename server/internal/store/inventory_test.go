package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestPurgeInventoryChangesHonorsRetention(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := enrolled(t, s, th("inv"))
	now := time.Now()
	mustExec(t, `INSERT INTO inventory_changes (host_id, module, kind, key, after, at) VALUES
	  ($1, 'ports', 'added', 'old', '{}', $2), ($1, 'ports', 'added', 'new', '{}', $3)`,
		id, now.Add(-91*24*time.Hour), now.Add(-89*24*time.Hour))

	n, err := s.PurgeInventoryChanges(ctx, now.Add(-90*24*time.Hour), 1000)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("filas purgadas got %d, want 1", n)
	}
	if got := count(t, `SELECT count(*) FROM inventory_changes WHERE key = 'new'`); got != 1 {
		t.Errorf("el cambio reciente debe sobrevivir, got %d", got)
	}
}

func TestInventoryHeartbeatNeedsMatchingPhoto(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := enrolled(t, s, th("inv-hb"))
	rev := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := s.InventoryHeartbeat(ctx, id, InventoryPorts, rev, time.Now()); !errors.Is(err, ErrNeedFull) {
		t.Fatalf("sin foto: err got %v, want ErrNeedFull", err)
	}
	in := InventoryFull{Module: InventoryPorts, Rev: rev, Items: []json.RawMessage{}, CollectedAt: time.Now()}
	if err := s.IngestInventory(ctx, id, in, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.InventoryHeartbeat(ctx, id, InventoryPorts, rev, time.Now()); err != nil {
		t.Errorf("latido con rev igual: %v", err)
	}
}
