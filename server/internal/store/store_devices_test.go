package store

import (
	"context"
	"errors"
	"net/netip"
	"reflect"
	"regexp"
	"testing"
	"time"
)

func mkDevice(t *testing.T, s *Store, name, addr, collector string, group *string) Device {
	t.Helper()
	d, err := s.CreateDevice(context.Background(), name, netip.MustParseAddr(addr), hostID(t, collector), group)
	if err != nil {
		t.Fatalf("creando dispositivo %q: %v", name, err)
	}
	return d
}

func mkPing(t *testing.T, s *Store, name string, scope AlertScope) ServiceCheckDef {
	t.Helper()
	d, err := s.CreateServiceCheck(context.Background(), ServiceCheckDef{
		ID: newCheckID(t), Name: name, Type: CheckPing, IntervalSeconds: 60, TimeoutMs: 2000, Retries: 1,
		WarnMs: iptr(100), CritMs: iptr(500), Scope: scope, Enabled: true,
	}, "")
	if err != nil {
		t.Fatalf("creando ping %q: %v", name, err)
	}
	return d
}

func pingResult(itemID, status string, ago time.Duration) CheckResult {
	return CheckResult{CheckID: itemID, Status: status, ResponseMs: 3, Detail: "ok", TS: time.Now().Add(-ago).Truncate(time.Second)}
}

func TestDeviceShapeConstraints(t *testing.T) {
	newTestStore(t)
	seedHost(t, "col", "up", ago(time.Second))
	col := hostID(t, "col")
	cases := []struct {
		name, sql string
	}{
		{"device sin colector", `INSERT INTO hosts (hostname, kind, address) VALUES ('d', 'device', '10.0.0.1')`},
		{"device sin address", `INSERT INTO hosts (hostname, kind, collector_host_id) VALUES ('d', 'device', '` + col + `')`},
		{"device con token de agente", `INSERT INTO hosts (hostname, kind, address, collector_host_id, agent_token_hash) VALUES ('d', 'device', '10.0.0.1', '` + col + `', 'h')`},
		{"agent con address", `INSERT INTO hosts (hostname, agent_token_hash, address) VALUES ('a', 'h2', '10.0.0.1')`},
		{"agent con colector", `INSERT INTO hosts (hostname, agent_token_hash, collector_host_id) VALUES ('a', 'h2', '` + col + `')`},
		{"agent sin token", `INSERT INTO hosts (hostname) VALUES ('a')`},
		{"address con mascara", `INSERT INTO hosts (hostname, kind, address, collector_host_id) VALUES ('d', 'device', '10.0.0.0/24', '` + col + `')`},
		{"kind invalido", `INSERT INTO hosts (hostname, agent_token_hash, kind) VALUES ('a', 'h2', 'otro')`},
	}
	for _, c := range cases {
		if _, err := testPool.Exec(context.Background(), c.sql); err == nil {
			t.Errorf("%s: got insert aceptado, want violacion de CHECK", c.name)
		}
	}
	if _, err := testPool.Exec(context.Background(),
		`INSERT INTO hosts (hostname, kind, address, collector_host_id) VALUES ('ok', 'device', '10.0.0.1', $1)`, col); err != nil {
		t.Errorf("device valido rechazado: %v", err)
	}
}

func TestDeviceNeverAuthenticates(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "col", "up", ago(time.Second))
	mkDevice(t, s, "d1", "10.0.0.1", "col", nil)
	// Ningun hash llega a un dispositivo (no tiene token), ni siquiera el de su colector.
	for _, h := range []string{th("col"), "hash-col", "", th("d1")} {
		id, err := s.HostIDByTokenHash(context.Background(), h)
		if h == "hash-col" {
			if err != nil || id != hostID(t, "col") {
				t.Errorf("colector con su token: got (%q, %v), want su id", id, err)
			}
			continue
		}
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("hash %q: got (%q, %v), want ErrNotFound", h, id, err)
		}
	}
	// La restriccion se aplica aunque alguien deje un hash en un dispositivo: la BD lo rechaza.
	if _, err := testPool.Exec(context.Background(), `UPDATE hosts SET agent_token_hash = 'x' WHERE hostname = 'd1'`); err == nil {
		t.Error("un dispositivo acepto un token de agente")
	}
}

func TestPingItemID(t *testing.T) {
	a := PingItemID("11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")
	b := PingItemID("11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")
	c := PingItemID("11111111-1111-1111-1111-111111111111", "33333333-3333-3333-3333-333333333333")
	if a != b {
		t.Errorf("no determinista: %q != %q", a, b)
	}
	if a == c {
		t.Errorf("pares distintos dieron el mismo id %q", a)
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(a) {
		t.Errorf("id %q no es un UUID v5", a)
	}
}

func TestDeviceCRUDAndErrors(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedHost(t, "col", "up", ago(time.Second))
	seedHost(t, "col2", "up", ago(time.Second))
	g := mkGroup(t, s, "lan", true, nil)
	setGroupOf(t, "col", g.ID) // el colector y sus dispositivos comparten grupo

	d := mkDevice(t, s, "router", "192.168.1.1", "col", &g.ID)
	if d.Status != "pending" || d.Address.String() != "192.168.1.1" || d.CollectorName != "col" || d.Group == nil || !d.CollectorReachable {
		t.Fatalf("crear: got %+v", d)
	}
	if _, err := s.CreateDevice(ctx, "dup", netip.MustParseAddr("192.168.1.1"), hostID(t, "col"), &g.ID); !errors.Is(err, ErrDeviceExists) {
		t.Errorf("misma IP y colector: got %v, want ErrDeviceExists", err)
	}
	// La misma IP tras otro colector es otra red: valida.
	other := mkDevice(t, s, "router-b", "192.168.1.1", "col2", nil)
	// Un dispositivo no puede ser colector; un colector inexistente tampoco.
	if _, err := s.CreateDevice(ctx, "x", netip.MustParseAddr("10.9.9.9"), d.ID, nil); !errors.Is(err, ErrCollectorInvalid) {
		t.Errorf("colector dispositivo: got %v, want ErrCollectorInvalid", err)
	}
	if _, err := s.CreateDevice(ctx, "x", netip.MustParseAddr("10.9.9.9"), missingUUID, nil); !errors.Is(err, ErrCollectorInvalid) {
		t.Errorf("colector inexistente: got %v, want ErrCollectorInvalid", err)
	}
	if _, err := s.CreateDevice(ctx, "x", netip.MustParseAddr("10.9.9.9"), hostID(t, "col"), sptr(missingUUID)); !errors.Is(err, ErrGroupNotFound) {
		t.Errorf("grupo inexistente: got %v, want ErrGroupNotFound", err)
	}

	// Cambiar la direccion lo devuelve a pending y deja el evento.
	mustExec(t, `UPDATE hosts SET status = 'up' WHERE id = $1`, d.ID)
	newAddr := netip.MustParseAddr("192.168.1.2")
	u, err := s.UpdateDevice(ctx, d.ID, DeviceChange{Address: &newAddr})
	if err != nil {
		t.Fatal(err)
	}
	if u.Status != "pending" || u.Address != newAddr {
		t.Errorf("update de direccion: got %+v, want pending y 192.168.1.2", u)
	}
	if n := count(t, `SELECT count(*) FROM host_events WHERE host_id = $1 AND from_status = 'up' AND to_status = 'pending'`, d.ID); n != 1 {
		t.Errorf("eventos up->pending got %d, want 1", n)
	}
	name := "core"
	// Quitar el grupo lo separaria del de su colector (lan): se rechaza y no cambia nada.
	if _, err := s.UpdateDevice(ctx, d.ID, DeviceChange{Name: &name, SetGroup: true}); !errors.Is(err, ErrCollectorGroupMismatch) {
		t.Errorf("update quitando el grupo: got %v, want ErrCollectorGroupMismatch", err)
	}
	if cur, err := s.DeviceByID(ctx, AllScope(), d.ID); err != nil || cur.Name != "router" || cur.Group == nil {
		t.Errorf("tras el rechazo: got (%+v, %v), want el dispositivo intacto", cur, err)
	}
	// Cambiar de colector a otro de distinto grupo tambien se rechaza; a uno del mismo grupo, no.
	col2 := hostID(t, "col2")
	if _, err := s.UpdateDevice(ctx, d.ID, DeviceChange{CollectorID: &col2}); !errors.Is(err, ErrCollectorGroupMismatch) {
		t.Errorf("update a un colector de otro grupo: got %v, want ErrCollectorGroupMismatch", err)
	}
	u, err = s.UpdateDevice(ctx, d.ID, DeviceChange{Name: &name})
	if err != nil || u.Name != "core" || u.Group == nil || u.CollectorGroupMismatch {
		t.Errorf("update de nombre: got (%+v, %v)", u, err)
	}
	if _, err := s.UpdateDevice(ctx, hostID(t, "col"), DeviceChange{Name: &name}); !errors.Is(err, ErrNotFound) {
		t.Errorf("update de un agente: got %v, want ErrNotFound", err)
	}
	if _, err := s.DeviceByID(ctx, AllScope(), hostID(t, "col")); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeviceByID de un agente: got %v, want ErrNotFound", err)
	}
	if _, err := s.DeleteDevice(ctx, hostID(t, "col")); !errors.Is(err, ErrNotFound) {
		t.Errorf("borrar un agente como dispositivo: got %v, want ErrNotFound", err)
	}

	// Lista con cursor, en orden por nombre.
	p, err := s.ListDevices(ctx, AllScope(), HostFilter{}, nil, 1)
	if err != nil || p.Total != 2 || len(p.Items) != 1 || p.NextCursor == nil || p.Items[0].Name != "core" {
		t.Fatalf("pagina 1: got (%+v, %v)", p, err)
	}
	p2, err := s.ListDevices(ctx, AllScope(), HostFilter{}, p.NextCursor, 1)
	if err != nil || len(p2.Items) != 1 || p2.Items[0].ID != other.ID || p2.NextCursor != nil {
		t.Fatalf("pagina 2: got (%+v, %v)", p2, err)
	}

	// Un colector con dispositivos no se puede borrar (RESTRICT); un dispositivo si.
	if _, err := testPool.Exec(ctx, `DELETE FROM hosts WHERE hostname = 'col'`); err == nil {
		t.Error("se borro un colector con dispositivos")
	}
	if n, err := s.DeleteDevice(ctx, d.ID); err != nil || n != "core" {
		t.Errorf("borrar: got (%q, %v)", n, err)
	}
}

func TestPingDeliveryAndIngest(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	always := mkGroup(t, s, "servidores", true, nil)
	lan := mkGroup(t, s, "lan", false, nil)
	// Cada colector solo sondea dispositivos de su propio grupo: col y col2 en lan, col-always en servidores.
	for _, h := range []struct{ name, group string }{{"col", lan.ID}, {"col2", lan.ID}, {"col-always", always.ID}} {
		seedHost(t, h.name, "up", ago(time.Second))
		setGroupOf(t, h.name, h.group)
	}
	d1 := mkDevice(t, s, "d1", "10.0.0.1", "col", &lan.ID)
	d2 := mkDevice(t, s, "d2", "10.0.0.2", "col-always", &always.ID)
	d3 := mkDevice(t, s, "d3", "10.0.0.3", "col", &lan.ID)
	foreign := mkDevice(t, s, "foreign", "10.0.1.1", "col2", &lan.ID)
	col, colAlways := hostID(t, "col"), hostID(t, "col-always")

	all := mkPing(t, s, "ping-all", AlertScope{Type: ScopeAll})
	grp := mkPing(t, s, "ping-lan", AlertScope{Type: ScopeGroup, GroupID: &lan.ID})
	one := mkPing(t, s, "ping-d2", AlertScope{Type: ScopeDevice, HostID: &d2.ID})
	tcp := mkCheck(t, s, "tcp-all", AlertScope{Type: ScopeAll})

	// Entrega: un item por par; los devices de otro colector (y de otro grupo) y los chequeos tcp no aparecen.
	delivered := func(collector string) map[string]bool {
		pings, err := s.CollectorPings(ctx, collector)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]bool{}
		for _, p := range pings {
			if p.ItemID != PingItemID(p.Def.ID, p.DeviceID) || !p.Address.IsValid() {
				t.Errorf("item %+v mal formado", p)
			}
			got[p.Def.Name+"@"+p.DeviceID] = true
		}
		return got
	}
	want := map[string]bool{
		"ping-all@" + d1.ID: true, "ping-all@" + d3.ID: true,
		"ping-lan@" + d1.ID: true, "ping-lan@" + d3.ID: true,
	}
	if got := delivered(col); !reflect.DeepEqual(got, want) {
		t.Errorf("entrega a col got %v, want %v", got, want)
	}
	wantAlways := map[string]bool{"ping-all@" + d2.ID: true, "ping-d2@" + d2.ID: true}
	if got := delivered(colAlways); !reflect.DeepEqual(got, wantAlways) {
		t.Errorf("entrega a col-always got %v, want %v", got, wantAlways)
	}
	if hc, err := s.HostChecks(ctx, col); err != nil || len(hc) != 1 || hc[0].ID != tcp.ID {
		t.Errorf("HostChecks del colector: got (%v, %v), want solo el tcp (un ping no va como chequeo propio)", hc, err)
	}
	if hc, err := s.HostServiceChecks(ctx, AllScope(), d1.ID); err != nil || len(hc) != 2 {
		t.Errorf("chequeos del dispositivo d1: got (%v, %v), want ping-all y ping-lan", hc, err)
	}
	if hc, err := s.HostServiceChecks(ctx, AllScope(), col); err != nil || len(hc) != 1 || hc[0].Def.ID != tcp.ID {
		t.Errorf("chequeos del colector: got (%v, %v), want solo el tcp", hc, err)
	}

	// Resultados: d1 responde, d2 no (grupo always_on), d3 solo unknown; el ping de otro colector, el id crudo del
	// chequeo y un id inventado se descartan.
	ingestResults(t, s, col,
		pingResult(PingItemID(all.ID, d1.ID), "up", 5*time.Second),
		pingResult(PingItemID(all.ID, d3.ID), "unknown", 5*time.Second),
		pingResult(PingItemID(all.ID, foreign.ID), "up", 5*time.Second),
		pingResult(all.ID, "up", 5*time.Second),
		pingResult(missingUUID, "up", 5*time.Second),
	)
	ingestResults(t, s, colAlways, pingResult(PingItemID(all.ID, d2.ID), "down", 5*time.Second))
	for name, want := range map[string]string{"d1": "up", "d2": "down", "d3": "pending", "foreign": "pending"} {
		if st, _ := hostState(t, name); st != want {
			t.Errorf("estado de %s got %q, want %q", name, st, want)
		}
	}
	if n := count(t, `SELECT count(*) FROM service_check_state WHERE host_id = $1 AND check_id = $2 AND status = 'up'`, d1.ID, all.ID); n != 1 {
		t.Errorf("estado del ping en d1: got %d filas, want 1", n)
	}
	if n := count(t, `SELECT count(*) FROM service_check_state WHERE host_id = $1`, col); n != 0 {
		t.Errorf("el colector guardo %d estados de ping, want 0", n)
	}
	if n := count(t, `SELECT count(*) FROM service_check_state`); n != 3 {
		t.Errorf("estados guardados got %d, want 3 (d1, d2, d3)", n)
	}
	if n := count(t, `SELECT count(*) FROM host_events WHERE host_id = $1 AND to_status = 'up' AND source = 'ingest'`, d1.ID); n != 1 {
		t.Errorf("evento pending->up de d1 got %d, want 1", n)
	}
	if n := count(t, `SELECT count(*) FROM hosts WHERE hostname = 'd1' AND last_seen_at > now() - interval '1 minute'`); n != 1 {
		t.Error("d1 no actualizo last_seen_at")
	}

	// Un resultado viejo del buffer offline (>5 min) guarda el estado del chequeo pero no mueve el dispositivo.
	ingestResults(t, s, col, pingResult(PingItemID(grp.ID, d3.ID), "up", 20*time.Minute))
	if st, _ := hostState(t, "d3"); st != "pending" {
		t.Errorf("d3 con un resultado de hace 20 min got %q, want pending", st)
	}
	// Un chequeo deshabilitado no llega ni acepta resultados.
	if _, err := s.UpdateServiceCheck(ctx, one.ID, func(d *ServiceCheckDef) error { d.Enabled = false; return nil }); err != nil {
		t.Fatal(err)
	}
	ingestResults(t, s, colAlways, pingResult(PingItemID(one.ID, d2.ID), "up", time.Second))
	if n := count(t, `SELECT count(*) FROM service_check_state WHERE check_id = $1`, one.ID); n != 0 {
		t.Errorf("resultado de un chequeo deshabilitado guardado: %d filas", n)
	}

	// d2 estaba down: un ping que responde lo devuelve a up.
	ingestResults(t, s, colAlways, pingResult(PingItemID(all.ID, d2.ID), "up", time.Second))
	if st, _ := hostState(t, "d2"); st != "up" {
		t.Errorf("d2 tras responder got %q, want up", st)
	}
}

func TestDetectorDevices(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	always := mkGroup(t, s, "servidores", true, nil)
	seedHost(t, "col", "up", ago(time.Second))
	seedHost(t, "col-caido", "up", ago(time.Hour)) // el detector lo marca down (grupo always_on)
	seedHost(t, "col-off", "offline", ago(time.Hour))
	for _, h := range []string{"col", "col-caido", "col-off"} { // colector y dispositivos comparten grupo
		setGroupOf(t, h, always.ID)
	}
	live := mkDevice(t, s, "live-silent", "10.0.0.1", "col", &always.ID)
	guarded := mkDevice(t, s, "live-unknown", "10.0.0.2", "col", &always.ID)
	orphan := mkDevice(t, s, "orphan", "10.0.0.3", "col-caido", &always.ID)
	orphan2 := mkDevice(t, s, "orphan-down", "10.0.0.4", "col-off", &always.ID)
	fresh := mkDevice(t, s, "fresh", "10.0.0.5", "col", &always.ID)
	for _, d := range []Device{live, guarded, orphan, orphan2, fresh} {
		mustExec(t, `UPDATE hosts SET status = 'up', last_seen_at = now() - interval '1 hour' WHERE id = $1`, d.ID)
	}
	mustExec(t, `UPDATE hosts SET status = 'down' WHERE id = $1`, orphan2.ID)
	mustExec(t, `UPDATE hosts SET last_seen_at = now() WHERE id = $1`, fresh.ID)
	// live-unknown: el colector lo tiene bloqueado (resultado unknown reciente) => sin evaluacion, no down.
	c := mkPing(t, s, "ping", AlertScope{Type: ScopeAll})
	mustExec(t, `INSERT INTO service_check_state (host_id, check_id, status, status_changed_at, ts, last_sample_at)
		VALUES ($1, $2, 'unknown', now(), now() - interval '10 seconds', now())`, guarded.ID, c.ID)

	stale, err := s.MarkStaleHosts(ctx, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	// Solo se devuelven caidas reales: live-silent y el colector caido (ambos en un grupo always_on).
	gotStale := map[string]string{}
	for _, h := range stale {
		gotStale[h.Hostname] = h.Status
	}
	if want := map[string]string{"live-silent": "down", "col-caido": "down"}; !reflect.DeepEqual(gotStale, want) {
		t.Errorf("hosts marcados got %v, want %v", gotStale, want)
	}
	for name, want := range map[string]string{
		"live-silent":  "down",    // colector vivo, dispositivo mudo, grupo always_on
		"live-unknown": "up",      // sin evaluar
		"orphan":       "offline", // colector caido: nunca down
		"orphan-down":  "offline", // ya estaba down y el colector esta offline: deja de contar como falla
		"fresh":        "up",
	} {
		if st, _ := hostState(t, name); st != want {
			t.Errorf("estado de %s got %q, want %q", name, st, want)
		}
	}
	if n := count(t, `SELECT count(*) FROM host_events WHERE host_id = $1 AND from_status = 'down' AND to_status = 'offline' AND source = 'detector'`, orphan2.ID); n != 1 {
		t.Errorf("evento down->offline del dispositivo sin colector got %d, want 1", n)
	}
	// Idempotente: una segunda pasada no cambia nada.
	if again, err := s.MarkStaleHosts(ctx, 5*time.Minute); err != nil || len(again) != 0 {
		t.Errorf("segunda pasada got (%v, %v), want vacia", again, err)
	}

	// El colector vuelve: su primer ping bueno devuelve el dispositivo a up.
	mustExec(t, `UPDATE hosts SET status = 'up', last_seen_at = now() WHERE hostname = 'col-caido'`)
	ingestResults(t, s, hostID(t, "col-caido"), pingResult(PingItemID(c.ID, orphan.ID), "up", time.Second))
	if st, _ := hostState(t, "orphan"); st != "up" {
		t.Errorf("dispositivo tras volver su colector got %q, want up", st)
	}
}

func TestDeviceScopeAndPingCheckVisibility(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ga, gb := mkGroup(t, s, "a", false, nil), mkGroup(t, s, "b", false, nil)
	seedHost(t, "col", "up", ago(time.Second)) // sin grupo
	seedHost(t, "col-a", "up", ago(time.Second))
	seedHost(t, "col-b", "up", ago(time.Second))
	setGroupOf(t, "col-a", ga.ID)
	setGroupOf(t, "col-b", gb.ID)
	da := mkDevice(t, s, "da", "10.0.0.1", "col-a", &ga.ID)
	db := mkDevice(t, s, "db", "10.0.0.2", "col-b", &gb.ID)
	dn := mkDevice(t, s, "dn", "10.0.0.3", "col", nil)
	pa := mkPing(t, s, "ping-da", AlertScope{Type: ScopeDevice, HostID: &da.ID})
	pb := mkPing(t, s, "ping-db", AlertScope{Type: ScopeDevice, HostID: &db.ID})

	viewer := GroupScope([]string{ga.ID})
	if p, err := s.ListDevices(ctx, viewer, HostFilter{}, nil, 50); err != nil || p.Total != 1 || len(p.Items) != 1 || p.Items[0].ID != da.ID {
		t.Errorf("lista del viewer de A: got (%+v, %v), want solo da", p, err)
	}
	for _, id := range []string{db.ID, dn.ID, missingUUID} {
		if _, err := s.DeviceByID(ctx, viewer, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("DeviceByID(%s) del viewer de A: got %v, want ErrNotFound", id, err)
		}
	}
	if _, err := s.DeviceByID(ctx, viewer, da.ID); err != nil {
		t.Errorf("DeviceByID propio: %v", err)
	}
	defs, err := s.ListServiceChecks(ctx, viewer)
	if err != nil || len(defs) != 1 || defs[0].ID != pa.ID {
		t.Errorf("chequeos visibles del viewer de A: got (%v, %v), want solo ping-da", defs, err)
	}
	if _, err := s.ServiceCheckByID(ctx, viewer, pb.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("chequeo de un dispositivo ajeno: got %v, want ErrNotFound", err)
	}
	if d, err := s.ServiceCheckByID(ctx, viewer, pa.ID); err != nil || d.DeviceName == nil || *d.DeviceName != "da" {
		t.Errorf("chequeo de un dispositivo propio: got (%+v, %v)", d, err)
	}
	// Apuntar a un host que no es dispositivo, o que no existe, se rechaza.
	for _, id := range []string{hostID(t, "col"), missingUUID} {
		_, err := s.CreateServiceCheck(ctx, ServiceCheckDef{
			ID: newCheckID(t), Name: "x" + id[:4], Type: CheckPing, IntervalSeconds: 60, TimeoutMs: 2000, Retries: 1,
			Scope: AlertScope{Type: ScopeDevice, HostID: &id}, Enabled: true}, "")
		if !errors.Is(err, ErrDeviceNotFound) {
			t.Errorf("alcance device %s: got %v, want ErrDeviceNotFound", id, err)
		}
	}
	// Alcance device en un chequeo que no es ping: lo rechaza la BD.
	if _, err := testPool.Exec(ctx, `UPDATE service_check_defs SET type = 'tcp' WHERE id = $1`, pa.ID); err == nil {
		t.Error("la BD acepto un chequeo tcp con alcance de dispositivo")
	}
}

func TestMigrateDownDevicesAndUpAgain(t *testing.T) {
	if testPool == nil {
		t.Skip("ARGOS_TEST_DATABASE_URL no definida")
	}
	ctx := context.Background()
	if err := resetSchema(ctx); err != nil {
		t.Fatal(err)
	}
	s := New(testPool)
	seedHost(t, "col", "up", ago(time.Second))
	mkDevice(t, s, "d1", "10.0.0.1", "col", nil)
	d1 := hostID(t, "d1")
	mkPing(t, s, "ping", AlertScope{Type: ScopeDevice, HostID: &d1})
	mkCheck(t, s, "tcp", AlertScope{Type: ScopeAll})

	provider, err := newProvider(testPool)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.DownTo(ctx, 22); err != nil {
		t.Fatalf("down a 22: %v", err)
	}
	if n := count(t, `SELECT count(*) FROM hosts`); n != 1 {
		t.Errorf("hosts tras el down got %d, want 1 (los dispositivos se pierden)", n)
	}
	if n := count(t, `SELECT count(*) FROM service_check_defs`); n != 1 {
		t.Errorf("chequeos tras el down got %d, want 1 (los ping se pierden)", n)
	}
	if err := Migrate(ctx, testPool); err != nil {
		t.Fatalf("up tras down: %v", err)
	}
}

func TestListHostsKindFilter(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedHost(t, "col", "up", ago(time.Second))
	mkDevice(t, s, "router", "192.168.1.1", "col", nil)

	tests := []struct {
		kinds []string
		want  []string
	}{
		{nil, []string{"col", "router"}},
		{[]string{"agent"}, []string{"col"}},
		{[]string{"device"}, []string{"router"}},
		{[]string{"agent", "device"}, []string{"col", "router"}},
	}
	for _, tt := range tests {
		page, err := s.ListHosts(ctx, AllScope(), HostFilter{Sort: "name", Limit: 100, Kinds: tt.kinds})
		if err != nil {
			t.Fatal(err)
		}
		got := make([]string, len(page.Items))
		for i, h := range page.Items {
			got[i] = h.Hostname
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("kinds %v: got %v, want %v", tt.kinds, got, tt.want)
		}
	}
}

func TestSummaryIgnoresDevices(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "col", "up", ago(time.Second))
	mkDevice(t, s, "router", "192.168.1.1", "col", nil)
	if _, err := s.pool.Exec(context.Background(), `UPDATE hosts SET status = 'down' WHERE kind = 'device'`); err != nil {
		t.Fatal(err)
	}
	sum, err := s.Summary(context.Background(), AllScope())
	if err != nil {
		t.Fatal(err)
	}
	if sum.Counts.Total() != 1 || sum.Counts.Down != 0 || len(sum.RecentlyDown) != 0 {
		t.Errorf("el resumen no debe contar dispositivos: %+v, caidos %v", sum.Counts, sum.RecentlyDown)
	}
	if sum.Devices.Total() != 1 || sum.Devices.Down != 1 {
		t.Errorf("los dispositivos van en su propio conteo: %+v", sum.Devices)
	}
}

func TestListDevicesFilters(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedHost(t, "col", "up", ago(time.Second))
	seedHost(t, "col-lima", "up", ago(time.Second))
	grp := mkGroup(t, s, "lima", false, nil)
	g := grp.ID
	setGroupOf(t, "col-lima", g)
	a := mkDevice(t, s, "router-a", "192.168.1.1", "col-lima", &g)
	mkDevice(t, s, "switch-b", "192.168.1.2", "col", nil)
	if _, err := s.pool.Exec(ctx, `UPDATE hosts SET status = 'down' WHERE id = $1`, a.ID); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		f    HostFilter
		want int
	}{
		{"sin filtro", HostFilter{}, 2},
		{"por nombre", HostFilter{Q: "ROUTER"}, 1},
		{"por estado", HostFilter{Statuses: []string{"down"}}, 1},
		{"por grupo", HostFilter{GroupIDs: []string{g}}, 1},
		{"sin grupo", HostFilter{NoGroup: true}, 1},
		{"estado y grupo sin coincidencia", HostFilter{Statuses: []string{"up"}, GroupIDs: []string{g}}, 0},
	}
	for _, tt := range tests {
		p, err := s.ListDevices(ctx, AllScope(), tt.f, nil, 50)
		if err != nil {
			t.Fatal(err)
		}
		if p.Total != tt.want || len(p.Items) != tt.want {
			t.Errorf("%s: total %d, items %d, want %d", tt.name, p.Total, len(p.Items), tt.want)
		}
	}
}

// Deshabilitar un dispositivo lo saca de la evaluacion (el colector ya no lo recibe, un ping no lo resucita, el
// detector no lo toca) y deja el evento; habilitarlo lo devuelve a pending. Cambiar direccion o colector no lo habilita.
func TestDeviceDisableAndEnable(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedHost(t, "col", "up", ago(time.Second))
	d := mkDevice(t, s, "router", "192.168.1.1", "col", nil)
	c := mkPing(t, s, "ping", AlertScope{Type: ScopeAll})
	mustExec(t, `UPDATE hosts SET status = 'up' WHERE id = $1`, d.ID)

	no, yes := false, true
	u, err := s.UpdateDevice(ctx, d.ID, DeviceChange{Enabled: &no})
	if err != nil || u.Status != "disabled" {
		t.Fatalf("deshabilitar got (%s, %v), want disabled", u.Status, err)
	}
	if n := count(t, `SELECT count(*) FROM host_events WHERE host_id = $1 AND from_status = 'up' AND to_status = 'disabled' AND source = 'admin'`, d.ID); n != 1 {
		t.Errorf("evento up->disabled got %d, want 1", n)
	}
	// Deshabilitar otra vez no repite el evento ni cambia nada.
	if u, err = s.UpdateDevice(ctx, d.ID, DeviceChange{Enabled: &no}); err != nil || u.Status != "disabled" {
		t.Fatalf("deshabilitar de nuevo got (%s, %v)", u.Status, err)
	}
	if n := count(t, `SELECT count(*) FROM host_events WHERE host_id = $1 AND to_status = 'disabled'`, d.ID); n != 1 {
		t.Errorf("eventos to disabled got %d, want 1 (idempotente)", n)
	}
	// Un ping bueno del colector no lo resucita.
	ingestResults(t, s, hostID(t, "col"), pingResult(PingItemID(c.ID, d.ID), "up", time.Second))
	if st, _ := hostState(t, "router"); st != "disabled" {
		t.Errorf("estado tras un ping got %q, want disabled", st)
	}
	// Mover el dispositivo deshabilitado (direccion) no lo habilita.
	addr := netip.MustParseAddr("192.168.1.9")
	if u, err = s.UpdateDevice(ctx, d.ID, DeviceChange{Address: &addr}); err != nil || u.Status != "disabled" {
		t.Errorf("cambiar la direccion de uno deshabilitado got (%s, %v), want disabled", u.Status, err)
	}
	// Habilitar: vuelve a pending.
	if u, err = s.UpdateDevice(ctx, d.ID, DeviceChange{Enabled: &yes}); err != nil || u.Status != "pending" {
		t.Fatalf("habilitar got (%s, %v), want pending", u.Status, err)
	}
	if n := count(t, `SELECT count(*) FROM host_events WHERE host_id = $1 AND from_status = 'disabled' AND to_status = 'pending' AND source = 'admin'`, d.ID); n != 1 {
		t.Errorf("evento disabled->pending got %d, want 1", n)
	}
	// Habilitar uno ya habilitado no cambia su estado.
	mustExec(t, `UPDATE hosts SET status = 'up' WHERE id = $1`, d.ID)
	if u, err = s.UpdateDevice(ctx, d.ID, DeviceChange{Enabled: &yes}); err != nil || u.Status != "up" {
		t.Errorf("habilitar uno activo got (%s, %v), want up", u.Status, err)
	}
	// Un agente no es un dispositivo: no se deshabilita por aqui.
	if _, err := s.UpdateDevice(ctx, hostID(t, "col"), DeviceChange{Enabled: &no}); !errors.Is(err, ErrNotFound) {
		t.Errorf("deshabilitar un agente got %v, want ErrNotFound", err)
	}
}

// Defensa en profundidad (hallazgo #1 de la revision multi-cliente): aunque un dispositivo haya quedado con un grupo
// distinto al de su colector (datos anteriores a H1, o un UPDATE directo), el colector NO lo recibe.
func TestCollectorIgnoresDevicesOfAnotherGroup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedHost(t, "col", "up", ago(time.Second))
	gA := mkGroup(t, s, "cliente-a", true, nil)
	gB := mkGroup(t, s, "cliente-b", true, nil)
	mustExec(t, `UPDATE hosts SET group_id = $1 WHERE hostname = 'col'`, gA.ID)
	d := mkDevice(t, s, "router", "192.168.1.1", "col", &gA.ID)
	mkPing(t, s, "ping", AlertScope{Type: ScopeAll})
	col := hostID(t, "col")

	got, err := s.CollectorPings(ctx, col)
	if err != nil || len(got) != 1 {
		t.Fatalf("mismo grupo: got %d asignaciones (%v), want 1", len(got), err)
	}
	// Desfase forzado por SQL (H1 lo impide por la API): el colector deja de recibirlo.
	mustExec(t, `UPDATE hosts SET group_id = $2 WHERE id = $1`, d.ID, gB.ID)
	if got, err = s.CollectorPings(ctx, col); err != nil || len(got) != 0 {
		t.Errorf("otro grupo: got %d asignaciones (%v), want 0", len(got), err)
	}
}
