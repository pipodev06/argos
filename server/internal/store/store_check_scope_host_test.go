package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestHostScopedCheckOnlyReachesThatHost(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	g := mkGroup(t, s, "g", false, nil)
	seedHost(t, "target", "up", ago(time.Second))
	seedHost(t, "mate", "up", ago(time.Second))
	seedHost(t, "col", "up", ago(time.Second))
	target, mate := hostID(t, "target"), hostID(t, "mate")
	for _, id := range []string{target, mate} {
		if _, err := s.SetHostGroup(ctx, id, &g.ID); err != nil {
			t.Fatal(err)
		}
	}
	c := mkCheck(t, s, "solo-target", AlertScope{Type: ScopeHost, HostID: &target})
	if c.Scope.Type != ScopeHost || c.Scope.HostID == nil || *c.Scope.HostID != target || c.DeviceName == nil || *c.DeviceName != "target" {
		t.Fatalf("lectura del alcance host got %+v name %v", c.Scope, c.DeviceName)
	}

	for host, want := range map[string][]string{"target": {"solo-target"}, "mate": nil, "col": nil} {
		got, err := s.HostChecks(ctx, hostID(t, host))
		if err != nil {
			t.Fatal(err)
		}
		if names := checkIDs(got); len(names) != len(want) || (len(want) > 0 && !reflect.DeepEqual(names, want)) {
			t.Errorf("%s: chequeos got %v, want %v", host, names, want)
		}
		hc, err := s.HostServiceChecks(ctx, AllScope(), hostID(t, host))
		if err != nil || len(hc) != len(want) {
			t.Errorf("%s: HostServiceChecks got (%d, %v), want %d", host, len(hc), err, len(want))
		}
	}

	// Un ambito restringido ve el chequeo de un host de su grupo y no el de un host ajeno.
	col := hostID(t, "col")
	foreign := mkCheck(t, s, "solo-col", AlertScope{Type: ScopeHost, HostID: &col})
	viewer := GroupScope([]string{g.ID})
	if defs, err := s.ListServiceChecks(ctx, viewer); err != nil || len(defs) != 1 || defs[0].ID != c.ID {
		t.Errorf("chequeos visibles del viewer got (%v, %v), want solo solo-target", defs, err)
	}
	if _, err := s.ServiceCheckByID(ctx, viewer, foreign.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("chequeo de un host ajeno got %v, want ErrNotFound", err)
	}
	if _, err := s.ServiceCheckByID(ctx, viewer, c.ID); err != nil {
		t.Errorf("chequeo de un host propio: %v", err)
	}

	// PATCH: pasar de host a all y de vuelta limpia y fija scope_host_id.
	got, err := s.UpdateServiceCheck(ctx, c.ID, func(d *ServiceCheckDef) error { d.Scope = AlertScope{Type: ScopeAll}; return nil })
	if err != nil || got.Scope.HostID != nil {
		t.Fatalf("host -> all got (%+v, %v)", got.Scope, err)
	}
	if got, err = s.UpdateServiceCheck(ctx, c.ID, func(d *ServiceCheckDef) error {
		d.Scope = AlertScope{Type: ScopeHost, HostID: &mate}
		return nil
	}); err != nil || got.Scope.HostID == nil || *got.Scope.HostID != mate {
		t.Fatalf("all -> host got (%+v, %v)", got.Scope, err)
	}

	// Estado y alerta sobre ese host: el resultado del host asignado se guarda; el del otro se descarta.
	ingestResults(t, s, target, result(c.ID, "up", time.Now().Add(-time.Minute)))
	if n := count(t, `SELECT count(*) FROM service_check_state WHERE host_id = $1`, target); n != 0 {
		t.Fatalf("el chequeo ya no es de target y no debe guardar estado, got %d", n)
	}
	if _, err := s.UpdateServiceCheck(ctx, c.ID, func(d *ServiceCheckDef) error {
		d.Scope = AlertScope{Type: ScopeHost, HostID: &target}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ingestResults(t, s, mate, result(c.ID, "down", time.Now().Add(-time.Minute)))
	if n := count(t, `SELECT count(*) FROM service_check_state WHERE host_id = $1`, mate); n != 0 {
		t.Fatalf("otro host del grupo no debe guardar estado, got %d", n)
	}
	ingestResults(t, s, target, result(c.ID, "down", time.Now().Add(-time.Minute)))
	if _, ok := stateOf(t, target, c.ID); !ok {
		t.Fatal("el host asignado debe guardar estado")
	}
	mkRule(t, s, svcRule("solo-target caido", c.ID, 0))
	setState(t, target, c.ID, "down", 5*time.Second, 10*time.Minute)
	if r := evalOnce(t, s); r.Opened != 1 {
		t.Fatalf("alerta service_check sobre host got opened %d, want 1", r.Opened)
	}
}

func TestHostScopeValidationAndConstraints(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedHost(t, "col", "up", ago(time.Second))
	mkDevice(t, s, "dev", "10.0.0.9", "col", nil)
	mkGroup(t, s, "algun-grupo", false, nil)
	// Un dispositivo o un host inexistente no sirve para el alcance host.
	for _, id := range []string{hostID(t, "dev"), missingUUID} {
		_, err := s.CreateServiceCheck(ctx, ServiceCheckDef{
			ID: newCheckID(t), Name: "x" + id[:4], Type: CheckTCP, IntervalSeconds: 60, TimeoutMs: 5000, Retries: 1,
			TargetHost: sptr("localhost"), TargetPort: iptr(22), Scope: AlertScope{Type: ScopeHost, HostID: &id}, Enabled: true}, "")
		if !errors.Is(err, ErrScopeHostNotFound) {
			t.Errorf("alcance host %s: got %v, want ErrScopeHostNotFound", id, err)
		}
	}
	// Un agente no sirve para el alcance device.
	col := hostID(t, "col")
	if _, err := s.CreateServiceCheck(ctx, ServiceCheckDef{
		ID: newCheckID(t), Name: "p", Type: CheckPing, IntervalSeconds: 60, TimeoutMs: 2000, Retries: 1,
		Scope: AlertScope{Type: ScopeDevice, HostID: &col}, Enabled: true}, ""); !errors.Is(err, ErrDeviceNotFound) {
		t.Errorf("device con un agente: got %v, want ErrDeviceNotFound", err)
	}
	// CHECKs de la BD: host con ping, host sin scope_host_id, host con grupo.
	c := mkCheck(t, s, "t", AlertScope{Type: ScopeHost, HostID: &col})
	for name, q := range map[string]string{
		"ping con host":       `UPDATE service_check_defs SET type = 'ping', target_host = NULL, target_port = NULL WHERE id = $1`,
		"host sin scope_host": `UPDATE service_check_defs SET scope_host_id = NULL WHERE id = $1`,
		"host con grupo":      `UPDATE service_check_defs SET scope_group_id = (SELECT id FROM host_groups LIMIT 1) WHERE id = $1`,
		"all con scope_host":  `UPDATE service_check_defs SET scope_type = 'all' WHERE id = $1`,
		"scope_type invalido": `UPDATE service_check_defs SET scope_type = 'otro' WHERE id = $1`,
	} {
		if _, err := testPool.Exec(ctx, q, c.ID); err == nil {
			t.Errorf("la BD acepto: %s", name)
		}
	}
	// Un host deshabilitado con chequeos asignados no rompe nada.
	mustExec(t, `UPDATE hosts SET status = 'disabled' WHERE id = $1`, col)
	if got, err := s.HostChecks(ctx, col); err != nil || len(got) != 1 {
		t.Errorf("HostChecks de un host deshabilitado got (%d, %v)", len(got), err)
	}
	if _, err := s.ListServiceChecks(ctx, AllScope()); err != nil {
		t.Errorf("ListServiceChecks: %v", err)
	}
	// Borrar el host borra sus chequeos (mismo tratamiento que device).
	mustExec(t, `DELETE FROM hosts WHERE kind = 'device'`)
	mustExec(t, `DELETE FROM hosts WHERE id = $1`, col)
	if n := count(t, `SELECT count(*) FROM service_check_defs WHERE scope_type = 'host'`); n != 0 {
		t.Errorf("chequeos host tras borrar el host got %d, want 0", n)
	}
}

func TestMigrateDownHostScopeAndUpAgain(t *testing.T) {
	if testPool == nil {
		t.Skip("ARGOS_TEST_DATABASE_URL no definida")
	}
	ctx := context.Background()
	if err := resetSchema(ctx); err != nil {
		t.Fatal(err)
	}
	s := New(testPool)
	seedHost(t, "h", "up", ago(time.Second))
	h := hostID(t, "h")
	mkCheck(t, s, "solo-h", AlertScope{Type: ScopeHost, HostID: &h})
	mkCheck(t, s, "todos", AlertScope{Type: ScopeAll})
	provider, err := newProvider(testPool)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.DownTo(ctx, 24); err != nil {
		t.Fatalf("down a 24: %v", err)
	}
	if n := count(t, `SELECT count(*) FROM service_check_defs`); n != 1 {
		t.Errorf("chequeos tras el down got %d, want 1 (los host se pierden)", n)
	}
	if err := Migrate(ctx, testPool); err != nil {
		t.Fatalf("up tras down: %v", err)
	}
}
