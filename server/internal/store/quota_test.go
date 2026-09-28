package store

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"
)

// setQuota fija una cuota del grupo (nil = sin limite). B7.
func setQuota(t *testing.T, s *Store, groupID string, resource quotaResource, n *int) {
	t.Helper()
	ch := GroupChange{}
	switch resource {
	case QuotaHosts:
		ch.MaxHostsSet, ch.MaxHosts = true, n
	case QuotaDevices:
		ch.MaxDevicesSet, ch.MaxDevices = true, n
	case QuotaChecks:
		ch.MaxChecksSet, ch.MaxChecks = true, n
	case QuotaUsers:
		ch.MaxUsersSet, ch.MaxUsers = true, n
	case QuotaRules:
		ch.MaxRulesSet, ch.MaxRules = true, n
	case QuotaChannels:
		ch.MaxChannelsSet, ch.MaxChannels = true, n
	}
	if _, err := s.UpdateGroup(context.Background(), groupID, ch); err != nil {
		t.Fatalf("fijando cuota de %s: %v", resource, err)
	}
}

func wantQuotaExceeded(t *testing.T, err error, resource quotaResource) {
	t.Helper()
	var qe *GroupQuotaError
	if !errors.As(err, &qe) {
		t.Fatalf("got %v, want *GroupQuotaError", err)
	}
	if qe.Resource != resource {
		t.Errorf("recurso got %q, want %q", qe.Resource, resource)
	}
}

// TestGroupQuotaHosts: max_hosts bloquea el alta de un host NUEVO en el grupo, pero un re-enroll (que
// conserva el host_id, spec B14/B15) no cuenta de nuevo aunque el grupo ya este en su tope.
func TestGroupQuotaHosts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	admin := mkUser(t, s, "admin", RoleAdmin)
	g := mkGroup(t, s, "cliente", false, nil)
	setQuota(t, s, g.ID, QuotaHosts, iptr(1))
	future := time.Now().Add(time.Hour)

	if err := mkEnrollToken(ctx, s, th("t1"), "host-1", admin, &g.ID, future, nil); err != nil {
		t.Fatal(err)
	}
	hostID, _, err := enrollHost(ctx, s, th("t1"), "linux", "amd64", "k1")
	if err != nil {
		t.Fatalf("primer host (dentro de la cuota): %v", err)
	}

	if err := mkEnrollToken(ctx, s, th("t2"), "host-2", admin, &g.ID, future, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := enrollHost(ctx, s, th("t2"), "linux", "amd64", "k2"); err == nil {
		t.Fatal("un segundo host nuevo debio chocar con la cuota")
	} else {
		wantQuotaExceeded(t, err, QuotaHosts)
	}

	// Re-enroll del host YA existente (spec B14/B15): no debe sumar, el grupo sigue en su tope de 1.
	if err := mkEnrollToken(ctx, s, th("t3"), "", admin, nil, future, &hostID); err != nil {
		t.Fatal(err)
	}
	if _, reenroll, err := enrollHost(ctx, s, th("t3"), "linux", "amd64", "k3"); err != nil || !reenroll {
		t.Errorf("re-enroll con el grupo lleno: got (reenroll=%v, err=%v), want (true, nil)", reenroll, err)
	}
}

// TestGroupQuotaDevices: max_devices bloquea un dispositivo NUEVO, pero moverlo a su MISMO grupo no cuenta.
func TestGroupQuotaDevices(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	g := mkGroup(t, s, "cliente", false, nil)
	seedHost(t, "col", "up", ago(time.Second))
	mustExec(t, `UPDATE hosts SET group_id = $1 WHERE hostname = 'col'`, g.ID)
	setQuota(t, s, g.ID, QuotaDevices, iptr(1))

	d1 := mkDevice(t, s, "dev-1", "10.0.0.1", "col", &g.ID)

	if _, err := s.CreateDevice(ctx, "dev-2", mustAddr(t, "10.0.0.2"), hostID(t, "col"), &g.ID); err == nil {
		t.Fatal("un segundo dispositivo debio chocar con la cuota")
	} else {
		wantQuotaExceeded(t, err, QuotaDevices)
	}

	// Re-fijar el MISMO grupo que ya tenia no debe sumar (spec BB, punto 2).
	if _, err := s.UpdateDevice(ctx, d1.ID, DeviceChange{SetGroup: true, GroupID: &g.ID}); err != nil {
		t.Errorf("mover al mismo grupo con la cuota llena: %v", err)
	}
}

// TestGroupQuotaUsers: max_users bloquea agregar un viewer NUEVO al grupo, pero conservar uno que ya tenia
// (al editar sin agregar grupos nuevos) no cuenta de nuevo.
func TestGroupQuotaUsers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	g := mkGroup(t, s, "cliente", false, nil)
	setQuota(t, s, g.ID, QuotaUsers, iptr(1))

	v1, err := s.CreateUser(ctx, NewUser{Username: "quota-v1", PasswordHash: "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA", Role: RoleViewer, GroupIDs: []string{g.ID}})
	if err != nil {
		t.Fatalf("primer viewer (dentro de la cuota): %v", err)
	}

	if _, err := s.CreateUser(ctx, NewUser{Username: "quota-v2", PasswordHash: "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA", Role: RoleViewer, GroupIDs: []string{g.ID}}); err == nil {
		t.Fatal("un segundo viewer en el grupo debio chocar con la cuota")
	} else {
		wantQuotaExceeded(t, err, QuotaUsers)
	}

	// Editar v1 sin agregarle un grupo nuevo (mismo group_ids) no debe sumar.
	if _, err := s.UpdateUser(ctx, v1.ID, UserChange{GroupIDs: &[]string{g.ID}}, time.Now()); err != nil {
		t.Errorf("conservar el mismo grupo con la cuota llena: %v", err)
	}
}

// TestGroupQuotaChecksRulesChannels: max_checks/max_rules/max_channels bloquean el alta de alcance
// `group`; un canal global (sin dueno) nunca cuenta.
func TestGroupQuotaChecksRulesChannels(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	g := mkGroup(t, s, "cliente", false, nil)
	zero := 0
	setQuota(t, s, g.ID, QuotaChecks, &zero)
	setQuota(t, s, g.ID, QuotaRules, &zero)
	setQuota(t, s, g.ID, QuotaChannels, &zero)

	if _, err := s.CreateServiceCheck(ctx, ServiceCheckDef{
		ID: newCheckID(t), Name: "chk", Type: CheckPing, IntervalSeconds: 60, TimeoutMs: 2000, Retries: 1,
		Scope: AlertScope{Type: ScopeGroup, GroupID: &g.ID}, Enabled: true,
	}, ""); err == nil {
		t.Error("chequeo de grupo con cuota 0 debio fallar")
	} else {
		wantQuotaExceeded(t, err, QuotaChecks)
	}

	rule := metricRule("r", "cpu_pct", "gt", 90, 60)
	rule.Scope = AlertScope{Type: ScopeGroup, GroupID: &g.ID}
	if _, err := s.CreateAlertRule(ctx, rule, ""); err == nil {
		t.Error("regla de grupo con cuota 0 debio fallar")
	} else {
		wantQuotaExceeded(t, err, QuotaRules)
	}

	owned := newChan("del cliente")
	owned.ID, owned.GroupID = "00000000-0000-4000-8000-000000000001", &g.ID
	if _, err := s.CreateChannel(ctx, owned, ""); err == nil {
		t.Error("canal de grupo con cuota 0 debio fallar")
	} else {
		wantQuotaExceeded(t, err, QuotaChannels)
	}

	// Un canal GLOBAL (sin dueno) nunca cuenta contra la cuota de ningun grupo.
	global := newChan("global")
	global.ID = "00000000-0000-4000-8000-000000000002"
	if _, err := s.CreateChannel(ctx, global, ""); err != nil {
		t.Errorf("canal global con la cuota del grupo en 0: %v", err)
	}
}

// TestGroupQuotaOnMove: mover un host YA existente a otro grupo (SetHostGroup/BulkHosts) tambien respeta
// max_hosts — deuda real corregida (no solo de test): antes solo el alta nueva y los dispositivos la tenian.
func TestGroupQuotaOnMove(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	dest := mkGroup(t, s, "destino", false, nil)
	setQuota(t, s, dest.ID, QuotaHosts, iptr(1))
	seedHost(t, "already-in", "up", ago(time.Second))
	mustExec(t, `UPDATE hosts SET group_id = $1 WHERE hostname = 'already-in'`, dest.ID)
	seedHost(t, "elsewhere", "up", ago(time.Second))

	// Re-fijar el MISMO grupo con la cuota ya llena no debe sumar.
	if _, err := s.SetHostGroup(ctx, hostID(t, "already-in"), &dest.ID); err != nil {
		t.Errorf("re-fijar el mismo grupo con la cuota llena: %v", err)
	}

	// Mover un host de OTRO grupo (o sin grupo) al destino lleno si debe chocar.
	if _, err := s.SetHostGroup(ctx, hostID(t, "elsewhere"), &dest.ID); err == nil {
		t.Error("mover un host nuevo al grupo lleno debio chocar con la cuota")
	} else {
		wantQuotaExceeded(t, err, QuotaHosts)
	}

	// BulkHosts (alta masiva de acciones) respeta la misma cuota.
	if _, err := s.BulkHosts(ctx, Bulk{IDs: []string{hostID(t, "elsewhere")}, Action: BulkSetGroup, GroupID: &dest.ID}); err == nil {
		t.Error("BulkHosts set_group al grupo lleno debio chocar con la cuota")
	} else {
		wantQuotaExceeded(t, err, QuotaHosts)
	}
}

func mustAddr(t *testing.T, s string) netip.Addr {
	t.Helper()
	a, err := netip.ParseAddr(s)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
