package store

import (
	"context"
	"testing"
	"time"
)

func TestSystemStats(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	empty, err := s.SystemStats(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("SystemStats: %v", err)
	}
	if empty.DBSizeBytes <= 0 || empty.QueuePending != 0 || empty.QueueOldestDue != nil || len(empty.Dead) != 0 {
		t.Fatalf("estado vacio got %+v", empty)
	}

	seedHost(t, "srv", "up", ago(time.Second))
	rule := mkRule(t, s, metricRule("cpu", "cpu_pct", "gt", 90, 0))
	chID := mkChannel(t, "hook", "warn", rule.ID)
	setLatest(t, "srv", 95)
	evalOnce(t, s)
	if got := deliveries(t, "opened", "pending"); got != 1 {
		t.Fatalf("entregas pending got %d, want 1", got)
	}

	st, err := s.SystemStats(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if st.QueuePending != 1 || st.QueueOldestDue == nil || len(st.Dead) != 0 {
		t.Fatalf("con una pendiente got %+v", st)
	}

	// Una entrega dead reciente cuenta; una de hace 3 dias no. El dueño del canal sale con su nombre.
	mustExec(t, `UPDATE notification_deliveries SET status = 'dead', dead_at = now()`)
	st, err = s.SystemStats(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if st.QueuePending != 0 || len(st.Dead) != 1 || st.Dead[0].ChannelID != chID || st.Dead[0].Count != 1 || st.Dead[0].Name != "hook" || st.Dead[0].GroupID != nil {
		t.Fatalf("con una dead got %+v", st)
	}
	var gid string
	if err := testPool.QueryRow(ctx, `INSERT INTO host_groups (name) VALUES ('Cliente A') RETURNING id`).Scan(&gid); err != nil {
		t.Fatal(err)
	}
	mustExec(t, `UPDATE notification_channels SET group_id = $1`, gid)
	st, err = s.SystemStats(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Dead) != 1 || st.Dead[0].GroupName == nil || *st.Dead[0].GroupName != "Cliente A" {
		t.Fatalf("dueño got %+v", st.Dead)
	}
	// Creada hace 3 dias pero muerta ahora: cuenta (la ventana mira cuando murio, no cuando se creo).
	mustExec(t, `UPDATE notification_deliveries SET created_at = now() - interval '3 days'`)
	st, err = s.SystemStats(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Dead) != 1 {
		t.Fatalf("dead reciente de una entrega vieja got %+v", st.Dead)
	}
	mustExec(t, `UPDATE notification_deliveries SET dead_at = now() - interval '3 days'`)
	st, err = s.SystemStats(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Dead) != 0 {
		t.Fatalf("dead fuera de la ventana got %+v", st.Dead)
	}
}
