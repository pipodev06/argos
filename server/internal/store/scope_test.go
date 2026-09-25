package store

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

// scopeFleet son dos grupos (A, B) con un host cada uno mas un host sin grupo.
type scopeFleet struct {
	groupA, groupB       string
	hostA, hostB, hostNo string
}

func seedScopeFleet(t *testing.T) scopeFleet {
	t.Helper()
	var f scopeFleet
	ctx := context.Background()
	if err := testPool.QueryRow(ctx, `INSERT INTO host_groups (name) VALUES ('A') RETURNING id`).Scan(&f.groupA); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `INSERT INTO host_groups (name) VALUES ('B') RETURNING id`).Scan(&f.groupB); err != nil {
		t.Fatal(err)
	}
	ins := func(name string, group *string) string {
		var id string
		err := testPool.QueryRow(ctx,
			`INSERT INTO hosts (hostname, agent_token_hash, status, last_seen_at, group_id) VALUES ($1, $2, 'down', now(), $3) RETURNING id`,
			name, th(name), group).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	f.hostA, f.hostB, f.hostNo = ins("host-a", &f.groupA), ins("host-b", &f.groupB), ins("host-none", nil)
	return f
}

func hostNames(page HostPage) []string {
	names := []string{}
	for _, h := range page.Items {
		names = append(names, h.Hostname)
	}
	slices.Sort(names)
	return names
}

func TestScopeRestrictsHostQueries(t *testing.T) {
	s := newTestStore(t)
	f := seedScopeFleet(t)
	ctx := context.Background()

	tests := []struct {
		name  string
		scope Scope
		want  []string
	}{
		{"todo", AllScope(), []string{"host-a", "host-b", "host-none"}},
		{"grupo A", GroupScope([]string{f.groupA}), []string{"host-a"}},
		{"grupos A y B", GroupScope([]string{f.groupB, f.groupA, f.groupA}), []string{"host-a", "host-b"}},
		{"sin grupos", GroupScope(nil), []string{}},
		{"valor cero no ve nada", Scope{}, []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			page, err := s.ListHosts(ctx, tc.scope, HostFilter{Sort: "name", Limit: 10})
			if err != nil {
				t.Fatal(err)
			}
			if got := hostNames(page); !slices.Equal(got, tc.want) {
				t.Errorf("hosts got %v, want %v", got, tc.want)
			}
			if len(tc.want) > 0 && page.Total != len(tc.want) {
				t.Errorf("total got %d, want %d", page.Total, len(tc.want))
			}

			sum, err := s.Summary(ctx, tc.scope)
			if err != nil {
				t.Fatal(err)
			}
			if sum.Counts.Total() != len(tc.want) || len(sum.RecentlyDown) != len(tc.want) {
				t.Errorf("summary got counts=%d down=%d, want %d", sum.Counts.Total(), len(sum.RecentlyDown), len(tc.want))
			}
		})
	}

	t.Run("filtros no amplian el ambito", func(t *testing.T) {
		scope := GroupScope([]string{f.groupA})
		for _, flt := range []HostFilter{
			{GroupIDs: []string{f.groupB}},
			{NoGroup: true},
			{GroupIDs: []string{f.groupB}, NoGroup: true},
		} {
			flt.Sort, flt.Limit = "name", 10
			page, err := s.ListHosts(ctx, scope, flt)
			if err != nil || len(page.Items) != 0 {
				t.Errorf("filtro %+v got (%+v, %v), want vacio", flt, page, err)
			}
		}
	})

	t.Run("detalle", func(t *testing.T) {
		scope := GroupScope([]string{f.groupA})
		if _, err := s.HostByID(ctx, scope, f.hostA); err != nil {
			t.Errorf("host propio: %v", err)
		}
		for _, id := range []string{f.hostB, f.hostNo, "0f8fad5b-d9cb-469f-a165-70867728950e"} {
			if _, err := s.HostByID(ctx, scope, id); !errors.Is(err, ErrNotFound) {
				t.Errorf("host %s fuera de ambito got %v, want ErrNotFound", id, err)
			}
		}
	})

	t.Run("grupos", func(t *testing.T) {
		groups, ungrouped, err := s.ListGroups(ctx, GroupScope([]string{f.groupA}))
		if err != nil || len(groups) != 1 || groups[0].Name != "A" || groups[0].HostCount != 1 || ungrouped != 0 {
			t.Errorf("restringido got (%+v, %d, %v)", groups, ungrouped, err)
		}
		groups, ungrouped, err = s.ListGroups(ctx, AllScope())
		if err != nil || len(groups) != 2 || ungrouped != 1 {
			t.Errorf("total got (%+v, %d, %v)", groups, ungrouped, err)
		}
	})
}

func TestUpdateUserGroupScope(t *testing.T) {
	s := newTestStore(t)
	f := seedScopeFleet(t)
	ctx := context.Background()
	now := time.Now()
	admin := mkUser(t, s, "admin", RoleAdmin)
	viewer := mkUser(t, s, "viewer", RoleViewer)
	yes, no := true, false

	u, err := s.UserByID(ctx, viewer)
	if err != nil || !u.AllGroups || u.CanInventory || len(u.GroupIDs) != 0 || !u.Scope().All() {
		t.Fatalf("defaults got (%+v, %v): viewer nuevo debe ver todo y sin inventario", u, err)
	}

	// Restringir a A: la sesion trae el ambito nuevo (sin cache).
	ids := []string{f.groupA}
	u, err = s.UpdateUser(ctx, viewer, UserChange{AllGroups: &no, GroupIDs: &ids, CanInventory: &yes}, now)
	if err != nil || u.AllGroups || !slices.Equal(u.GroupIDs, ids) || !u.CanInventory {
		t.Fatalf("restringir got (%+v, %v)", u, err)
	}
	h := make([]byte, 32)
	if err := s.CreateSession(ctx, Session{IDHash: h, UserID: viewer, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	si, err := s.SessionByHash(ctx, h)
	if err != nil || si.User.Scope().Key() != GroupScope(ids).Key() || !si.User.CanInventory {
		t.Fatalf("sesion got (%+v, %v)", si.User, err)
	}

	// Reemplazar por B y luego por lista vacia (ve nada, valido).
	ids = []string{f.groupB}
	if u, err = s.UpdateUser(ctx, viewer, UserChange{GroupIDs: &ids}, now); err != nil || !slices.Equal(u.GroupIDs, ids) || u.AllGroups {
		t.Fatalf("reemplazar got (%+v, %v)", u, err)
	}
	empty := []string{}
	if u, err = s.UpdateUser(ctx, viewer, UserChange{GroupIDs: &empty}, now); err != nil || len(u.GroupIDs) != 0 || u.Scope().All() {
		t.Fatalf("vacio got (%+v, %v)", u, err)
	}

	// Grupo inexistente: ErrGroupNotFound.
	ghost := []string{"0f8fad5b-d9cb-469f-a165-70867728950e"}
	if _, err := s.UpdateUser(ctx, viewer, UserChange{GroupIDs: &ghost}, now); !errors.Is(err, ErrGroupNotFound) {
		t.Errorf("grupo inexistente got %v, want ErrGroupNotFound", err)
	}

	// Borrar el grupo quita el acceso (cascade), no lo amplia.
	ids = []string{f.groupA}
	if _, err = s.UpdateUser(ctx, viewer, UserChange{GroupIDs: &ids}, now); err != nil {
		t.Fatal(err)
	}
	mustExec(t, `DELETE FROM hosts WHERE group_id = $1`, f.groupA)
	if err := s.DeleteGroup(ctx, f.groupA); err != nil {
		t.Fatal(err)
	}
	if u, err = s.UserByID(ctx, viewer); err != nil || u.AllGroups || len(u.GroupIDs) != 0 {
		t.Errorf("tras borrar el grupo got (%+v, %v), want restringido sin grupos", u, err)
	}

	// group_ids con all_groups=true es incoherente; all_groups=true solo limpia la lista.
	ids = []string{f.groupB}
	if _, err := s.UpdateUser(ctx, viewer, UserChange{AllGroups: &yes, GroupIDs: &ids}, now); !errors.Is(err, ErrInvalidScope) {
		t.Errorf("all_groups + group_ids got %v, want ErrInvalidScope", err)
	}
	if u, err = s.UpdateUser(ctx, viewer, UserChange{AllGroups: &yes}, now); err != nil || !u.AllGroups || len(u.GroupIDs) != 0 {
		t.Errorf("volver a todos got (%+v, %v)", u, err)
	}

	// Un admin no se restringe.
	for name, ch := range map[string]UserChange{
		"all_groups=false": {AllGroups: &no},
		"can_inventory":    {CanInventory: &yes},
		"group_ids":        {GroupIDs: &ids},
	} {
		if _, err := s.UpdateUser(ctx, admin, ch, now); !errors.Is(err, ErrInvalidScope) {
			t.Errorf("admin con %s got %v, want ErrInvalidScope", name, err)
		}
	}
	if u, err := s.UserByID(ctx, admin); err != nil || !u.AllGroups || u.CanInventory || !u.Scope().All() {
		t.Errorf("admin got (%+v, %v), want intacto", u, err)
	}
	// Ni siquiera a la vez que se le promueve.
	role := RoleAdmin
	if _, err := s.UpdateUser(ctx, viewer, UserChange{Role: &role, AllGroups: &no}, now); !errors.Is(err, ErrInvalidScope) {
		t.Errorf("promover y restringir got %v, want ErrInvalidScope", err)
	}
}

func TestPromoteRestrictedViewerResetsScope(t *testing.T) {
	s := newTestStore(t)
	f := seedScopeFleet(t)
	ctx := context.Background()
	mkUser(t, s, "admin", RoleAdmin)
	viewer := mkUser(t, s, "viewer", RoleViewer)
	no, yes := false, true
	ids := []string{f.groupA}
	if _, err := s.UpdateUser(ctx, viewer, UserChange{AllGroups: &no, GroupIDs: &ids, CanInventory: &yes}, time.Now()); err != nil {
		t.Fatal(err)
	}
	role := RoleAdmin
	u, err := s.UpdateUser(ctx, viewer, UserChange{Role: &role}, time.Now())
	if err != nil || !u.AllGroups || u.CanInventory || len(u.GroupIDs) != 0 || !u.Scope().All() {
		t.Fatalf("promocion got (%+v, %v), want ambito reiniciado", u, err)
	}
	if n := count(t, `SELECT count(*) FROM user_groups WHERE user_id = $1`, viewer); n != 0 {
		t.Errorf("filas en user_groups got %d, want 0", n)
	}
}

func TestSchemaRejectsScopedAdmin(t *testing.T) {
	s := newTestStore(t)
	admin := mkUser(t, s, "admin", RoleAdmin)
	if _, err := testPool.Exec(context.Background(), `UPDATE users SET all_groups = false WHERE id = $1`, admin); err == nil {
		t.Error("el CHECK debe rechazar un admin con all_groups=false")
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE users SET can_inventory = true WHERE id = $1`, admin); err == nil {
		t.Error("el CHECK debe rechazar un admin con can_inventory=true")
	}
}
