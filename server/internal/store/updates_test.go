package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

var updatesRev = strings.Repeat("c", 64)

func i64ptr(n int64) *int64 { return &n }

// snap arma un envio de parches; rev distinto por sufijo para simular cambios.
func snap(rev string, total, sec *int, reboot *bool, at time.Time) UpdatesSnapshot {
	return UpdatesSnapshot{Rev: strings.Repeat(rev, 64), PendingTotal: total, PendingSecurity: sec, RebootRequired: reboot,
		Source: "apt", Advisories: []string{}, CollectedAt: at}
}

func historyCount(t *testing.T, hostID string) int {
	t.Helper()
	return count(t, `SELECT count(*) FROM updates_history WHERE host_id = $1`, hostID)
}

func TestUpdatesUnknownIsNotZero(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := enrolled(t, s, th("upd-null"))
	now := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	in := UpdatesSnapshot{Rev: updatesRev, Source: "none", CollectedAt: now, Advisories: nil}
	if err := s.IngestUpdates(ctx, id, in, now); err != nil {
		t.Fatal(err)
	}
	got, found, err := s.HostUpdates(ctx, AllScope(), id)
	if err != nil || !found {
		t.Fatalf("HostUpdates got (found=%v, err=%v)", found, err)
	}
	if got.PendingTotal != nil || got.PendingSecurity != nil || got.RebootRequired != nil || got.LastInstalled != nil || got.CacheAgeS != nil {
		t.Errorf("desconocido debe ser nil, got %+v", got)
	}
	if got.RebootSince != nil || got.SecurityPendingSince != nil {
		t.Errorf("sin datos no hay marcas de tiempo, got %v %v", got.RebootSince, got.SecurityPendingSince)
	}
	if got.Advisories == nil || len(got.Advisories) != 0 {
		t.Errorf("advisories got %v, want lista vacia", got.Advisories)
	}
	// Un cero medido SI se guarda como cero.
	in2 := snap("d", iptr(0), iptr(0), bptr(false), now.Add(time.Hour))
	if err := s.IngestUpdates(ctx, id, in2, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, _, err = s.HostUpdates(ctx, AllScope(), id)
	if err != nil || got.PendingTotal == nil || *got.PendingTotal != 0 || got.RebootRequired == nil || *got.RebootRequired {
		t.Errorf("cero medido got %+v (err %v), want 0/false no nulos", got, err)
	}
}

func TestUpdatesSinceMarks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := enrolled(t, s, th("upd-since"))
	t0 := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	steps := []struct {
		name         string
		rev          string
		sec          *int
		reboot       *bool
		at           time.Time
		wantReboot   *time.Time
		wantSecurity *time.Time
	}{
		{"primer envio con reinicio y seguridad", "1", iptr(2), bptr(true), t0, &t0, &t0},
		{"sigue pendiente: conserva las marcas", "2", iptr(3), bptr(true), t0.Add(time.Hour), &t0, &t0},
		{"desconocido: se limpian (no cuenta como pendiente)", "3", nil, nil, t0.Add(2 * time.Hour), nil, nil},
		{"vuelve a true: marca nueva", "4", iptr(1), bptr(true), t0.Add(3 * time.Hour), tp(t0.Add(3 * time.Hour)), tp(t0.Add(3 * time.Hour))},
		{"false y cero: se limpian", "5", iptr(0), bptr(false), t0.Add(4 * time.Hour), nil, nil},
	}
	for _, st := range steps {
		in := snap(st.rev, iptr(10), st.sec, st.reboot, st.at)
		if err := s.IngestUpdates(ctx, id, in, st.at); err != nil {
			t.Fatalf("%s: %v", st.name, err)
		}
		got, _, err := s.HostUpdates(ctx, AllScope(), id)
		if err != nil {
			t.Fatal(err)
		}
		if !sameTime(got.RebootSince, st.wantReboot) || !sameTime(got.SecurityPendingSince, st.wantSecurity) {
			t.Errorf("%s: reboot_since=%v security_since=%v, want %v %v", st.name, got.RebootSince, got.SecurityPendingSince, st.wantReboot, st.wantSecurity)
		}
	}
}

func tp(t time.Time) *time.Time { return &t }

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

func TestUpdatesResendSameRevIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := enrolled(t, s, th("upd-idem"))
	t0 := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	in := snap("a", iptr(5), iptr(1), bptr(true), t0)
	for i := range 3 {
		if err := s.IngestUpdates(ctx, id, in, t0.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if n := historyCount(t, id); n != 1 {
		t.Errorf("historial tras 3 envios iguales got %d, want 1 (solo la linea base)", n)
	}
	got, _, err := s.HostUpdates(ctx, AllScope(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.RebootSince == nil || !got.RebootSince.Equal(t0) {
		t.Errorf("reboot_since got %v, want %v (no se mueve al reenviar)", got.RebootSince, t0)
	}
	if !got.ReceivedAt.Equal(t0.Add(2 * time.Hour)) {
		t.Errorf("received_at got %v, want el ultimo envio", got.ReceivedAt)
	}
	// Un cambio de contadores agrega un punto; el orden es del mas reciente al mas viejo.
	if err := s.IngestUpdates(ctx, id, snap("b", iptr(4), iptr(1), bptr(true), t0), t0.Add(5*time.Hour)); err != nil {
		t.Fatal(err)
	}
	pts, err := s.HostUpdatesHistory(ctx, AllScope(), id, 50)
	if err != nil || len(pts) != 2 || *pts[0].PendingTotal != 4 || *pts[1].PendingTotal != 5 {
		t.Errorf("historial got %+v (err %v), want [4, 5]", pts, err)
	}
}

func TestUpdatesHeartbeat(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := enrolled(t, s, th("upd-hb"))
	t0 := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	if err := s.InventoryHeartbeat(ctx, id, InventoryUpdates, updatesRev, t0); !errors.Is(err, ErrNeedFull) {
		t.Fatalf("sin foto got %v, want ErrNeedFull", err)
	}
	in := snap("c", iptr(1), iptr(0), bptr(false), t0)
	if err := s.IngestUpdates(ctx, id, in, t0); err != nil {
		t.Fatal(err)
	}
	if err := s.InventoryHeartbeat(ctx, id, InventoryUpdates, in.Rev, t0.Add(time.Hour)); err != nil {
		t.Errorf("latido con el rev correcto: %v", err)
	}
	if err := s.InventoryHeartbeat(ctx, id, InventoryUpdates, strings.Repeat("e", 64), t0.Add(time.Hour)); !errors.Is(err, ErrNeedFull) {
		t.Errorf("latido con otro rev got %v, want ErrNeedFull", err)
	}
	if !ValidInventoryModule(InventoryUpdates) {
		t.Error("updates debe ser un modulo valido")
	}
}

func TestUpdatesHistoryDailyCapAndRetention(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := enrolled(t, s, th("upd-cap"))
	day := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	for i := range maxUpdatesHistoryPerDay + 20 {
		in := snap("a", iptr(i), nil, nil, day)
		in.Rev = fmt.Sprintf("%064x", i)
		if err := s.IngestUpdates(ctx, id, in, day.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	if n := historyCount(t, id); n != maxUpdatesHistoryPerDay {
		t.Errorf("historial del dia got %d, want tope %d", n, maxUpdatesHistoryPerDay)
	}
	// La foto actual sigue al dia aunque el historial se haya topado.
	if got, _, _ := s.HostUpdates(ctx, AllScope(), id); got.PendingTotal == nil || *got.PendingTotal != maxUpdatesHistoryPerDay+19 {
		t.Errorf("foto actual got %+v, want el ultimo envio", got.PendingTotal)
	}
	// Retencion: solo se borra lo anterior al corte.
	mustExec(t, `UPDATE updates_history SET at = $2 WHERE host_id = $1 AND id IN (SELECT id FROM updates_history WHERE host_id = $1 ORDER BY id LIMIT 10)`,
		id, day.Add(-401*24*time.Hour))
	n, err := s.PurgeUpdatesHistory(ctx, day.Add(-400*24*time.Hour), 1000)
	if err != nil || n != 10 {
		t.Errorf("purga got (%d, %v), want 10", n, err)
	}
	if got := historyCount(t, id); got != maxUpdatesHistoryPerDay-10 {
		t.Errorf("historial tras purga got %d, want %d", got, maxUpdatesHistoryPerDay-10)
	}
}

func TestUpdatesReadIsScoped(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := enrolled(t, s, th("upd-scope"))
	t0 := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	if _, found, err := s.HostUpdates(ctx, AllScope(), id); err != nil || found {
		t.Errorf("host sin datos got (found=%v, err=%v), want no encontrado sin error", found, err)
	}
	if err := s.IngestUpdates(ctx, id, snap("a", iptr(1), iptr(1), bptr(false), t0), t0); err != nil {
		t.Fatal(err)
	}
	// El host no tiene grupo: un ambito por grupos (o vacio) no lo ve y no distingue de un host inexistente.
	for _, sc := range []Scope{GroupScope(nil), GroupScope([]string{"0f8fad5b-d9cb-469f-a165-70867728950e"}), {}} {
		if _, _, err := s.HostUpdates(ctx, sc, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("HostUpdates fuera de ambito got %v, want ErrNotFound", err)
		}
		if _, err := s.HostUpdatesHistory(ctx, sc, id, 10); !errors.Is(err, ErrNotFound) {
			t.Errorf("HostUpdatesHistory fuera de ambito got %v, want ErrNotFound", err)
		}
		if _, _, err := s.HostOSEOL(ctx, sc, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("HostOSEOL fuera de ambito got %v, want ErrNotFound", err)
		}
	}
}

func TestOSEOLKeyResolution(t *testing.T) {
	st := newTestStore(t)
	tests := []struct {
		name, os, osName, version string
		family, cycle             string // vacio = sin coincidencia
	}{
		{"windows 11 25H2", "windows", "Microsoft Windows 11 Pro", "10.0.26200.6584 Build 26200", "windows-11", "26200"},
		{"windows 11 23H2", "windows", "Microsoft Windows 11 Home", "10.0.22631.4460 Build 22631", "windows-11", "22631"},
		{"windows 10 22H2", "windows", "Microsoft Windows 10 Pro", "10.0.19045.5011 Build 19045", "windows-10", "19045"},
		{"windows server 2022", "windows", "Microsoft Windows Server 2022 Datacenter", "10.0.20348.2113 Build 20348", "windows-server", "2022"},
		{"windows server 2012 R2", "windows", "Microsoft Windows Server 2012 R2 Standard", "6.3.9600 Build 9600", "windows-server", "2012-r2"},
		{"windows version rara", "windows", "Microsoft Windows 11 Pro", "raro", "", ""},
		{"ubuntu 22.04", "linux", "ubuntu", "22.04", "ubuntu", "22.04"},
		{"ubuntu con parche", "linux", "Ubuntu", "24.04.1", "ubuntu", "24.04"},
		{"debian 12", "linux", "debian", "12.5", "debian", "12"},
		{"debian testing", "linux", "debian", "trixie/sid", "", ""},
		{"rhel 9", "linux", "redhat", "9.3", "rhel", "9"},
		{"rocky 9", "linux", "rocky", "9.4", "rocky", "9"},
		{"alma 8", "linux", "almalinux", "8.10", "almalinux", "8"},
		{"centos 7", "linux", "centos", "7.9.2009", "centos", "7"},
		{"macos 14", "darwin", "macOS", "14.5", "macos", "14"},
		{"windows 11 25H2 (el agente reporta la version como 25H2)", "windows", "Microsoft Windows 11 Pro", "25H2", "windows-11", "26200"},
		{"windows 11 24H2", "windows", "Microsoft Windows 11 Home", "24H2", "windows-11", "26100"},
		{"windows 10 22H2", "windows", "Microsoft Windows 10 Pro", "22H2", "windows-10", "19045"},
		{"windows 11 por build", "windows", "Microsoft Windows 11 Pro", "10.0.26200.9457", "windows-11", "26200"},
		{"windows 11 con version desconocida", "windows", "Microsoft Windows 11 Pro", "26H9", "", ""},
		{"distro desconocida", "linux", "arch", "rolling", "", ""},
		{"sin datos", "", "", "", "", ""},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var fam, cyc string
			err := testPool.QueryRow(context.Background(),
				`SELECT coalesce(min(family), ''), coalesce(min(cycle), '') FROM os_eol_key($1, $2, $3)`,
				tc.os, tc.osName, tc.version).Scan(&fam, &cyc)
			if err != nil {
				t.Fatal(err)
			}
			if fam != tc.family || cyc != tc.cycle {
				t.Errorf("got (%q, %q), want (%q, %q)", fam, cyc, tc.family, tc.cycle)
			}
			// La clave precomputada por trigger (host nuevo + host_info) es identica a la de la funcion.
			id := enrolled(t, st, th(fmt.Sprintf("eolkey-%d", i)))
			mustExec(t, `UPDATE hosts SET os = NULLIF($2, '') WHERE id = $1`, id, tc.os)
			mustExec(t, `INSERT INTO host_info (host_id, os_name, os_version) VALUES ($1, $2, $3)`, id, tc.osName, tc.version)
			var pf, pc string
			if err := testPool.QueryRow(context.Background(),
				`SELECT coalesce(eol_family, ''), coalesce(eol_cycle, '') FROM hosts WHERE id = $1`, id).Scan(&pf, &pc); err != nil {
				t.Fatal(err)
			}
			if pf != tc.family || pc != tc.cycle {
				t.Errorf("clave precomputada got (%q, %q), want (%q, %q)", pf, pc, tc.family, tc.cycle)
			}
		})
	}
}

func TestOSEOLSeedAndHostResolution(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	// Toda fila de la semilla tiene fecha valida y una fuente reconocida; las familias son las que resuelve os_eol_key.
	if n := count(t, `SELECT count(*) FROM os_eol WHERE family NOT IN ('windows-10','windows-11','windows-server','ubuntu','debian','rhel','centos','rocky','almalinux','macos')`); n != 0 {
		t.Errorf("familias fuera de las que resuelve os_eol_key: %d", n)
	}
	if n := count(t, `SELECT count(*) FROM os_eol`); n < 40 {
		t.Errorf("filas de la semilla got %d, want >= 40", n)
	}
	id := enrolled(t, s, th("eol-host"))
	mustExec(t, `UPDATE hosts SET os = 'linux' WHERE id = $1`, id)
	mustExec(t, `INSERT INTO host_info (host_id, os_name, os_version) VALUES ($1, 'ubuntu', '22.04')`, id)
	got, found, err := s.HostOSEOL(ctx, AllScope(), id)
	if err != nil || !found {
		t.Fatalf("HostOSEOL got (found=%v, err=%v)", found, err)
	}
	if got.Family != "ubuntu" || got.Cycle != "22.04" || !got.LTS || got.Estimated || got.EOLDate.Format(time.DateOnly) != "2027-06-01" {
		t.Errorf("EOL got %+v, want ubuntu 22.04 LTS 2027-06-01 oficial", got)
	}
	// Una version que no esta en la tabla no es un error: sin dato de fin de vida.
	mustExec(t, `UPDATE host_info SET os_version = '19.10' WHERE host_id = $1`, id)
	if n := count(t, `SELECT count(*) FROM hosts WHERE id = '`+id+`' AND eol_family = 'ubuntu' AND eol_cycle = '19.10'`); n != 1 {
		t.Errorf("el trigger no actualizo la clave al cambiar os_version")
	}
	if _, found, err := s.HostOSEOL(ctx, AllScope(), id); err != nil || found {
		t.Errorf("version fuera de tabla got (found=%v, err=%v), want no encontrado sin error", found, err)
	}
	// Cambiar hosts.os y borrar host_info tambien recalculan.
	mustExec(t, `DELETE FROM host_info WHERE host_id = $1`, id)
	if n := count(t, `SELECT count(*) FROM hosts WHERE id = '`+id+`' AND eol_family IS NULL`); n != 1 {
		t.Errorf("el trigger no limpio la clave al borrar host_info")
	}
	mustExec(t, `UPDATE hosts SET os = 'darwin' WHERE id = $1`, id)
	mustExec(t, `INSERT INTO host_info (host_id, os_name, os_version) VALUES ($1, 'macOS', '14.5')`, id)
	if got, found, err := s.HostOSEOL(ctx, AllScope(), id); err != nil || !found || got.Family != "macos" {
		t.Errorf("tras cambiar hosts.os got (%+v, found=%v, err=%v), want macos", got, found, err)
	}
	// Un host sin host_info tampoco falla.
	id2 := enrolled(t, s, th("eol-host-2"))
	if _, found, err := s.HostOSEOL(ctx, AllScope(), id2); err != nil || found {
		t.Errorf("host sin host_info got (found=%v, err=%v), want no encontrado sin error", found, err)
	}
}

func TestCanUpdatesPermission(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	viewer := mkUser(t, s, "upd-viewer", RoleViewer)
	admin := mkUser(t, s, "upd-admin", RoleAdmin)

	u, err := s.UpdateUser(ctx, viewer, UserChange{CanUpdates: bptr(true)}, time.Now())
	if err != nil || !u.CanUpdates || u.CanInventory {
		t.Fatalf("viewer con can_updates got %+v (err %v)", u, err)
	}
	// El permiso llega a la sesion (ver SessionByHash) y se conserva al cambiar otra cosa.
	u, err = s.UpdateUser(ctx, viewer, UserChange{CanInventory: bptr(true)}, time.Now())
	if err != nil || !u.CanUpdates || !u.CanInventory {
		t.Errorf("can_updates se perdio al cambiar otro permiso: %+v (err %v)", u, err)
	}
	// Un admin no lo lleva: pedirlo es invalido y el CHECK de la BD lo impide aunque se salte la app.
	if _, err := s.UpdateUser(ctx, admin, UserChange{CanUpdates: bptr(true)}, time.Now()); !errors.Is(err, ErrInvalidScope) {
		t.Errorf("admin con can_updates got %v, want ErrInvalidScope", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE users SET can_updates = true WHERE id = $1`, admin); err == nil {
		t.Error("el CHECK users_scope_viewer_only debe rechazar can_updates en un admin")
	}
	// Promover a admin reinicia el permiso.
	u, err = s.UpdateUser(ctx, viewer, UserChange{Role: ptrStr(RoleAdmin)}, time.Now())
	if err != nil || u.CanUpdates || u.CanInventory || !u.AllGroups {
		t.Errorf("promover a admin got %+v (err %v), want sin permisos de lectura y todos los grupos", u, err)
	}
}

func ptrStr(s string) *string { return &s }

func TestUpdatesSyntheticLoad1000Hosts(t *testing.T) {
	if testing.Short() {
		t.Skip("carga sintetica omitida con -short")
	}
	s := newTestStore(t)
	ctx := context.Background()
	mustExec(t, `INSERT INTO hosts (hostname, agent_token_hash, status)
	             SELECT 'load-' || g, 'hash-load-' || g, 'up' FROM generate_series(1, 1000) g`)
	rows, err := testPool.Query(ctx, `SELECT id FROM hosts WHERE hostname LIKE 'load-%' ORDER BY hostname`)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	advisories := make([]string, 200)
	for i := range advisories {
		advisories[i] = fmt.Sprintf("KB%07d", 5000000+i)
	}
	t0 := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	start := time.Now()
	for i, id := range ids {
		in := UpdatesSnapshot{Rev: fmt.Sprintf("%064x", i), PendingTotal: iptr(i%40 + 10), PendingSecurity: iptr(i % 7), RebootRequired: bptr(i%3 == 0),
			Source: "wua", CacheAgeS: i64ptr(3600), Advisories: advisories, CollectedAt: t0}
		if err := s.IngestUpdates(ctx, id, in, t0); err != nil {
			t.Fatal(err)
		}
		// Segundo envio con cambio: agrega historial.
		in.Rev = fmt.Sprintf("%064x", i+100000)
		in.PendingTotal = iptr(i%40 + 11)
		if err := s.IngestUpdates(ctx, id, in, t0.Add(6*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	elapsed := time.Since(start)
	var cur, hist int64
	if err := testPool.QueryRow(ctx, `SELECT pg_total_relation_size('updates_current'), pg_total_relation_size('updates_history')`).Scan(&cur, &hist); err != nil {
		t.Fatal(err)
	}
	t.Logf("1000 hosts x 2 envios (200 avisos cada uno): %v; updates_current %d KiB, updates_history %d KiB", elapsed.Round(time.Millisecond), cur>>10, hist>>10)
	if n := count(t, `SELECT count(*) FROM updates_current`); n != 1000 {
		t.Errorf("filas de updates_current got %d, want 1000", n)
	}
	if n := count(t, `SELECT count(*) FROM updates_history`); n != 2000 {
		t.Errorf("filas de updates_history got %d, want 2000", n)
	}
}
