package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"argos/server/internal/store"
)

// Aislamiento entre clientes (grupos): colector y dispositivo del mismo grupo (H1) y baja de un cliente por purga (H2).

// setGroupAPI asigna el grupo de un host por la API y comprueba la respuesta.
func (s *scopeEnv) setGroupAPI(hostID, groupID string, status int, code string) *httptest.ResponseRecorder {
	s.t.Helper()
	rec := s.do("PUT", "/api/v1/hosts/"+hostID+"/group", `{"group_id":"`+groupID+`"}`, withCookie(s.admin))
	s.wantStatus(rec, status, code)
	return rec
}

// --- H1 ---

// Un agente colector del cliente A nunca recibe dispositivos, IP ni credenciales SNMP de un grupo B, y la API rechaza
// crear o mover dispositivos y colectores de forma que se crucen.
func TestCollectorNeverServesAnotherGroupsDevices(t *testing.T) {
	x := newSNMPEnv(t)
	colA, tokA, _ := x.enrollHost(x.admin, "col-a")
	colB, tokB, _ := x.enrollHost(x.admin, "col-b")
	x.setGroupAPI(colA, x.groupA, http.StatusOK, "")
	x.setGroupAPI(colB, x.groupB, http.StatusOK, "")
	prof := x.newProfile(v3Body("core"))
	devA := x.newDevice(fmt.Sprintf(`{"name":"sw-a","address":"10.1.0.1","collector_id":%q,"group_id":%q,"snmp_profile_id":%q}`, colA, x.groupA, prof))
	devB := x.newDevice(fmt.Sprintf(`{"name":"sw-b","address":"10.2.0.1","collector_id":%q,"group_id":%q,"snmp_profile_id":%q}`, colB, x.groupB, prof))
	for _, b := range []string{`{"name":"ping-all","type":"ping"}`, `{"name":"snmp-up","type":"snmp","metric":"reachable"}`} {
		x.wantStatus(x.do("POST", "/api/v1/service-checks", b, withCookie(x.admin)), http.StatusCreated, "")
	}

	for _, c := range []struct{ tok, own, foreign, foreignAddr string }{
		{tokA, devA, devB, "10.2.0.1"}, {tokB, devB, devA, "10.1.0.1"},
	} {
		x.clock.Advance(11 * time.Second)
		rec := x.do("POST", "/api/v1/agent/metrics", `{"metrics":{"cpu_pct":1},"checks_rev":"","services":[]}`, withHeader("Authorization", "Bearer "+c.tok))
		x.wantStatus(rec, http.StatusOK, "")
		var resp reportResp
		x.dataAs(rec, &resp)
		if resp.Checks == nil || len(resp.Checks.Items) != 2 { // un ping y un snmp, del dispositivo propio
			t.Fatalf("entrega got %+v, want 2 items (ping y snmp del dispositivo propio)", resp.Checks)
		}
		for _, it := range resp.Checks.Items {
			if it["device_id"] != c.own {
				t.Errorf("item de otro dispositivo: %v, want device_id %s", it, c.own)
			}
		}
		if body := rec.Body.String(); strings.Contains(body, c.foreign) || strings.Contains(body, c.foreignAddr) {
			t.Errorf("la entrega al colector filtra datos del otro grupo: %.400s", body)
		}
	}

	// Crear: colector de otro grupo, o con/sin grupo distinto.
	mismatch := func(b string) {
		t.Helper()
		rec := x.do("POST", "/api/v1/devices", b, withCookie(x.admin))
		x.wantStatus(rec, http.StatusUnprocessableEntity, "collector_group_mismatch")
	}
	mismatch(fmt.Sprintf(`{"name":"x","address":"10.9.0.1","collector_id":%q,"group_id":%q}`, colA, x.groupB))
	mismatch(fmt.Sprintf(`{"name":"x","address":"10.9.0.1","collector_id":%q}`, colA))
	mismatch(fmt.Sprintf(`{"name":"x","address":"10.9.0.1","collector_id":%q,"group_id":%q}`, x.hostNo, x.groupA))

	// Mover: un dispositivo sin su colector (422) y un colector sin sus dispositivos (409), con la cuenta.
	rec := x.do("PUT", "/api/v1/hosts/"+devA+"/group", `{"group_id":"`+x.groupB+`"}`, withCookie(x.admin))
	x.wantStatus(rec, http.StatusUnprocessableEntity, "collector_group_mismatch")
	rec = x.setGroupAPI(colA, x.groupB, http.StatusConflict, "collector_has_devices")
	if !strings.Contains(rec.Body.String(), `"details":{"devices":1}`) {
		t.Errorf("el 409 debe llevar la cuenta de dispositivos afectados: %s", rec.Body.String())
	}
	bulk := func(ids []string, status int, code string) {
		t.Helper()
		b := fmt.Sprintf(`{"ids":["%s"],"action":"set_group","group_id":%q}`, strings.Join(ids, `","`), x.groupB)
		x.wantStatus(x.do("POST", "/api/v1/hosts/bulk", b, withCookie(x.admin)), status, code)
	}
	bulk([]string{colA}, http.StatusConflict, "collector_has_devices")
	bulk([]string{devA}, http.StatusUnprocessableEntity, "collector_group_mismatch")
	if got := x.count(`SELECT count(*) FROM hosts WHERE id IN ($1, $2) AND group_id = $3`, colA, devA, x.groupA); got != 2 {
		t.Errorf("un rechazo no debe cambiar nada: %d de 2 siguen en A", got)
	}
	// Moverlos a la vez si vale.
	bulk([]string{colA, devA}, http.StatusOK, "")
	if got := x.count(`SELECT count(*) FROM hosts WHERE id IN ($1, $2) AND group_id = $3`, colA, devA, x.groupB); got != 2 {
		t.Errorf("mover colector y dispositivo a la vez: %d de 2 en B, want 2", got)
	}

	// Un dato anterior desfasado se marca para el admin (y el diagnostico lo cuenta), sin migrarlo.
	mustExec(t, `UPDATE hosts SET group_id = $2 WHERE id = $1`, devB, x.groupA)
	n, err := x.st.CollectorGroupMismatches(context.Background())
	if err != nil || n != 1 {
		t.Errorf("CollectorGroupMismatches got (%d, %v), want 1", n, err)
	}
	if body := x.do("GET", "/api/v1/devices/"+devB, "", withCookie(x.admin)).Body.String(); !strings.Contains(body, `"collector_group_mismatch":true`) {
		t.Errorf("el detalle del dispositivo desfasado debe marcar collector_group_mismatch: %.300s", body)
	}
	if body := x.do("GET", "/api/v1/devices/"+devA, "", withCookie(x.admin)).Body.String(); strings.Contains(body, "collector_group_mismatch") {
		t.Errorf("un dispositivo consistente no debe llevar la marca: %.300s", body)
	}
	// Renombrar un desfasado no se bloquea; cambiarle el grupo a otro que sigue distinto del colector, si.
	x.wantStatus(x.do("PATCH", "/api/v1/devices/"+devB, `{"name":"sw-b2"}`, withCookie(x.admin)), http.StatusOK, "")
	x.wantStatus(x.do("PATCH", "/api/v1/devices/"+devB, `{"group_id":null}`, withCookie(x.admin)), http.StatusUnprocessableEntity, "collector_group_mismatch")
	x.wantStatus(x.do("PATCH", "/api/v1/devices/"+devB, `{"group_id":"`+x.groupB+`"}`, withCookie(x.admin)), http.StatusOK, "")
}

// --- H2 ---

// rowsOf cuenta las filas de todas las tablas que referencian al host (columnas host_id, scope_host_id y
// collector_host_id): una tabla nueva con host_id que no se purgue haria fallar los tests de purga.
func rowsOf(t *testing.T, hostID string) int {
	t.Helper()
	ctx := context.Background()
	rows, err := testPool.Query(ctx, `SELECT table_name, column_name FROM information_schema.columns
		WHERE table_schema = 'public' AND column_name IN ('host_id', 'scope_host_id', 'collector_host_id')
		  AND table_name IN (SELECT tablename FROM pg_tables WHERE schemaname = 'public') ORDER BY 1, 2`)
	if err != nil {
		t.Fatal(err)
	}
	type tc struct{ table, col string }
	var cols []tc
	for rows.Next() {
		var c tc
		if err := rows.Scan(&c.table, &c.col); err != nil {
			t.Fatal(err)
		}
		cols = append(cols, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(cols) < 15 {
		t.Fatalf("tablas con referencia a hosts: %d, want >= 15 (la consulta de esquema fallo)", len(cols))
	}
	total := 0
	for _, c := range cols {
		var n int
		// Identificadores del catalogo (no entrada de usuario).
		if err := testPool.QueryRow(ctx, fmt.Sprintf(`SELECT count(*)::int FROM %q WHERE %q = $1`, c.table, c.col), hostID).Scan(&n); err != nil {
			t.Fatalf("contando %s.%s: %v", c.table, c.col, err)
		}
		total += n
	}
	n := 0
	if err := testPool.QueryRow(ctx, `SELECT count(*)::int FROM hosts WHERE id = $1`, hostID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return total + n
}

// seedHostData da al host una fila en cada tabla sin CASCADE (metrics, metrics_hourly, metrics_chunks, disk_samples,
// disk_chunks, service_checks) ademas de lo que ya tenga (host_latest, host_info y host_events del alta).
func seedHostData(t *testing.T, hostID string) {
	t.Helper()
	mustExec(t, `INSERT INTO metrics (host_id, ts, cpu_pct, mem_used_mb, mem_total_mb) VALUES ($1, now(), 5, 100, 200)`, hostID)
	mustExec(t, `INSERT INTO metrics_hourly (host_id, hour, samples) VALUES ($1, date_trunc('hour', now()) - interval '3 hours', 10)`, hostID)
	mustExec(t, `INSERT INTO metrics_chunks (host_id, day, n, codec, data) VALUES ($1, current_date - 20, 0, 1, '\x00')`, hostID)
	mustExec(t, `INSERT INTO disk_samples (host_id, ts, used_mb, total_mb, mount) VALUES ($1, now(), 1, 2, '/')`, hostID)
	mustExec(t, `INSERT INTO disk_chunks (host_id, day, mount_count, n, codec, data) VALUES ($1, current_date - 20, 0, 0, 1, '\x00')`, hostID)
	mustExec(t, `INSERT INTO service_checks (host_id, name, status, ts) VALUES ($1, 'x', 'up', now())`, hostID)
}

func TestPurgeHostAPI(t *testing.T) {
	x := newScopeEnv(t)
	colA, tokA, _ := x.enrollHost(x.admin, "col-a")
	x.setGroupAPI(colA, x.groupA, http.StatusOK, "")
	dev := x.newDevice(fmt.Sprintf(`{"name":"sw","address":"10.1.0.1","collector_id":%q,"group_id":%q}`, colA, x.groupA))
	for _, id := range []string{colA, x.hostA, x.hostB} { // un dispositivo no genera metricas
		seedHostData(t, id)
	}
	if rowsOf(t, x.hostA) < 6 || rowsOf(t, colA) < 6 {
		t.Fatalf("siembra insuficiente: hostA %d filas, colA %d", rowsOf(t, x.hostA), rowsOf(t, colA))
	}
	beforeB := rowsOf(t, x.hostB)
	purge := func(id, body string) *httptest.ResponseRecorder {
		return x.do("POST", "/api/v1/hosts/"+id+"/purge", body, withCookie(x.admin))
	}

	// Validaciones: id malo, sin confirmacion, nombre distinto, inexistente y un dispositivo (se borra con DELETE).
	x.wantStatus(purge("no-uuid", `{"confirm_hostname":"x"}`), http.StatusBadRequest, "invalid_id")
	x.wantStatus(purge(x.hostA, `{}`), http.StatusBadRequest, "invalid_body")
	x.wantStatus(purge(x.hostA, `{"confirm_hostname":"HOST-A"}`), http.StatusBadRequest, "confirm_mismatch")
	x.wantStatus(purge(x.hostA, `{"confirm_hostname":"host-a","extra":1}`), http.StatusBadRequest, "invalid_body")
	x.wantStatus(purge(missingID, `{"confirm_hostname":"x"}`), http.StatusNotFound, "not_found")
	x.wantStatus(purge(dev, `{"confirm_hostname":"sw"}`), http.StatusNotFound, "not_found")
	x.wantStatus(x.do("POST", "/api/v1/hosts/"+x.hostA+"/purge", `{"confirm_hostname":"host-a"}`, withCookie(x.va)), http.StatusForbidden, "")
	// Un colector con dispositivos no se purga.
	rec := purge(colA, `{"confirm_hostname":"col-a"}`)
	x.wantStatus(rec, http.StatusConflict, "collector_has_devices")
	if !strings.Contains(rec.Body.String(), `"details":{"devices":1}`) {
		t.Errorf("el 409 debe llevar la cuenta: %s", rec.Body.String())
	}
	if rowsOf(t, x.hostA) == 0 {
		t.Fatal("una purga rechazada borro datos")
	}

	// Purga del host A: sin rastro suyo, con B intacto.
	rec = purge(x.hostA, `{"confirm_hostname":"host-a"}`)
	x.wantStatus(rec, http.StatusOK, "")
	for _, k := range []string{`"metrics":2`, `"metrics_hourly":1`, `"metrics_chunks":1`, `"disk_samples":1`, `"disk_chunks":1`, `"service_checks":1`} {
		if !strings.Contains(rec.Body.String(), k) {
			t.Errorf("la respuesta debe informar %s: %s", k, rec.Body.String())
		}
	}
	if n := rowsOf(t, x.hostA); n != 0 {
		t.Errorf("quedan %d filas del host purgado", n)
	}
	if n := rowsOf(t, x.hostB); n != beforeB {
		t.Errorf("la purga de A cambio las filas de B: %d, want %d", n, beforeB)
	}
	x.wantStatus(x.do("GET", "/api/v1/hosts/"+x.hostA, "", withCookie(x.admin)), http.StatusNotFound, "not_found")
	if body := x.do("GET", "/api/v1/hosts", "", withCookie(x.admin)).Body.String(); strings.Contains(body, "host-a") {
		t.Errorf("el host purgado sigue en la lista: %.300s", body)
	}
	// Repetir la purga es un 404 (idempotente en el efecto).
	x.wantStatus(purge(x.hostA, `{"confirm_hostname":"host-a"}`), http.StatusNotFound, "not_found")
	// Auditoria: con conteos y sin el nombre del host.
	audit := x.do("GET", "/api/v1/audit?event=host_purged", "", withCookie(x.admin)).Body.String()
	if !strings.Contains(audit, "host_purged") || strings.Contains(audit, "host-a") {
		t.Errorf("auditoria de la purga: %.400s", audit)
	}

	// Sin dispositivos, el colector si se puede purgar (y su token deja de valer).
	x.wantStatus(x.do("DELETE", "/api/v1/devices/"+dev, "", withCookie(x.admin)), http.StatusOK, "")
	x.wantStatus(purge(colA, `{"confirm_hostname":"col-a"}`), http.StatusOK, "")
	x.wantStatus(x.metrics(tokA), http.StatusUnauthorized, "invalid_token")
	if n := rowsOf(t, colA); n != 0 {
		t.Errorf("quedan %d filas del colector purgado", n)
	}
}

func TestPurgeGroupAPI(t *testing.T) {
	x := newAlertScopeEnv(t)
	colA, tokA, _ := x.enrollHost(x.admin, "col-a")
	x.setGroupAPI(colA, x.groupA, http.StatusOK, "")
	devA := x.newDevice(fmt.Sprintf(`{"name":"sw-a","address":"10.1.0.1","collector_id":%q,"group_id":%q}`, colA, x.groupA))
	devB := x.newDevice(fmt.Sprintf(`{"name":"sw-b","address":"10.2.0.1","collector_id":%q,"group_id":%q}`, x.hostB, x.groupB))
	aIDs, bIDs := []string{x.hostA, colA, devA}, []string{x.hostB, devB}
	for _, id := range append(append([]string{}, aIDs...), bIDs...) {
		seedHostData(t, id)
	}
	// Configuracion con alcance A y B, un enroll token de A y un viewer con A y B.
	ruleA := x.ae.createRule(fmt.Sprintf(`{"name":"r-a","kind":"host_status","duration_seconds":0,"severity":"info","scope":{"type":"group","group_id":%q}}`, x.groupA))
	ruleB := x.ae.createRule(fmt.Sprintf(`{"name":"r-b","kind":"host_status","duration_seconds":0,"severity":"info","scope":{"type":"group","group_id":%q}}`, x.groupB))
	winA := x.ae.createWindow(fmt.Sprintf(`{"name":"m-a","scope":{"type":"group","group_id":%q}}`, x.groupA), time.Hour)
	winB := x.ae.createWindow(fmt.Sprintf(`{"name":"m-b","scope":{"type":"group","group_id":%q}}`, x.groupB), time.Hour)
	chkA := x.newCheck(fmt.Sprintf(`{"name":"tcp-a","type":"tcp","target_host":"localhost","target_port":22,"scope":{"type":"group","group_id":%q}}`, x.groupA))
	chkB := x.newCheck(fmt.Sprintf(`{"name":"tcp-b","type":"tcp","target_host":"localhost","target_port":22,"scope":{"type":"group","group_id":%q}}`, x.groupB))
	x.wantStatus(x.do("POST", "/api/v1/hosts/enroll-token", fmt.Sprintf(`{"hostname":"nuevo-a","group_id":%q}`, x.groupA), withCookie(x.admin)), http.StatusCreated, "")
	// Canal propio de A (se borra), uno de B (intacto) y uno global sin grupo (nunca se toca).
	chanA := "22222222-2222-4222-8222-222222222221"
	chanB := "22222222-2222-4222-8222-222222222222"
	chanGlobal := "22222222-2222-4222-8222-222222222223"
	mustExec(t, `INSERT INTO notification_channels (id, name, kind, group_id) VALUES ($1, 'hook-a', 'webhook', $2)`, chanA, x.groupA)
	mustExec(t, `INSERT INTO notification_channels (id, name, kind, group_id) VALUES ($1, 'hook-b', 'webhook', $2)`, chanB, x.groupB)
	mustExec(t, `INSERT INTO notification_channels (id, name, kind) VALUES ($1, 'hook-global', 'webhook')`, chanGlobal)
	// Perfil SNMP exclusivo de devA (se borra) y uno compartido con devB, de B (sobrevive: sigue en uso ajeno).
	devA2 := x.newDevice(fmt.Sprintf(`{"name":"sw-a2","address":"10.1.0.2","collector_id":%q,"group_id":%q}`, colA, x.groupA))
	aIDs = append(aIDs, devA2)
	profExclusive := "33333333-3333-4333-8333-333333333331"
	profShared := "33333333-3333-4333-8333-333333333332"
	mustExec(t, `INSERT INTO snmp_profiles (id, name, version, secret_enc, secret_key_id) VALUES ($1, 'p-exclusive', 2, '\x01', 'k')`, profExclusive)
	mustExec(t, `INSERT INTO snmp_profiles (id, name, version, secret_enc, secret_key_id) VALUES ($1, 'p-shared', 2, '\x01', 'k')`, profShared)
	mustExec(t, `UPDATE hosts SET snmp_profile_id = $1 WHERE id = $2`, profExclusive, devA2)
	mustExec(t, `UPDATE hosts SET snmp_profile_id = $1 WHERE id = $2`, profShared, devA)
	mustExec(t, `UPDATE hosts SET snmp_profile_id = $1 WHERE id = $2`, profShared, devB)
	vabID := x.newUser("viewab", pwViewer, store.RoleViewer, false).ID
	x.patchUser(vabID, fmt.Sprintf(`{"all_groups":false,"group_ids":[%q,%q]}`, x.groupA, x.groupB), http.StatusOK, "")
	vab := x.login("viewab", pwViewer)
	beforeB := rowsOf(t, x.hostB) + rowsOf(t, devB)

	purge := func(c *http.Cookie, id, body string) *httptest.ResponseRecorder {
		return x.do("POST", "/api/v1/groups/"+id+"/purge", body, withCookie(c))
	}
	x.wantStatus(purge(x.admin, "no-uuid", `{"confirm_name":"A"}`), http.StatusBadRequest, "invalid_body")
	x.wantStatus(purge(x.admin, x.groupA, `{}`), http.StatusBadRequest, "invalid_body")
	x.wantStatus(purge(x.admin, x.groupA, `{"confirm_name":"a"}`), http.StatusBadRequest, "confirm_mismatch")
	x.wantStatus(purge(x.admin, x.groupA, `{"confirm_name":"A","otro":1}`), http.StatusBadRequest, "invalid_body")
	x.wantStatus(purge(x.admin, missingID, `{"confirm_name":"A"}`), http.StatusNotFound, "not_found")
	x.wantStatus(purge(x.va, x.groupA, `{"confirm_name":"A"}`), http.StatusForbidden, "")

	// Otra purga en curso: 409 y sin efecto.
	x.api.purgeSem <- struct{}{}
	x.wantStatus(purge(x.admin, x.groupA, `{"confirm_name":"A"}`), http.StatusConflict, "purge_in_progress")
	<-x.api.purgeSem
	if rowsOf(t, x.hostA) == 0 {
		t.Fatal("una purga rechazada borro datos")
	}

	rec := purge(x.admin, x.groupA, `{"confirm_name":"A","delete_group":true,"disable_users":true}`)
	x.wantStatus(rec, http.StatusOK, "")
	var res struct {
		Agents, Devices        int
		Failed                 []string `json:"failed_host_ids"`
		Rules, Windows, Checks int
		EnrollTokens           int                     `json:"enroll_tokens"`
		Channels               int                     `json:"channels"`
		SnmpProfiles           int                     `json:"snmp_profiles"`
		UsersDisabled          int                     `json:"users_disabled"`
		UsersScopeReduced      int                     `json:"users_scope_reduced"`
		GroupDeleted           bool                    `json:"group_deleted"`
		DeletedRows            struct{ Metrics int64 } `json:"deleted_rows"`
	}
	x.dataAs(rec, &res)
	if res.Agents != 2 || res.Devices != 2 || len(res.Failed) != 0 || res.Rules != 1 || res.Windows != 1 || res.Checks != 1 ||
		res.EnrollTokens != 1 || res.UsersDisabled != 1 || res.UsersScopeReduced != 2 || !res.GroupDeleted || res.DeletedRows.Metrics < 3 ||
		res.Channels != 1 || res.SnmpProfiles != 1 {
		t.Errorf("conteos de la purga: %+v", res)
	}
	if got := x.count(`SELECT count(*) FROM notification_channels WHERE id = ANY($1)`, []string{chanA, chanB, chanGlobal}); got != 2 {
		t.Errorf("canales tras la purga de A: %d, want 2 (B y global)", got)
	}
	if got := x.count(`SELECT count(*) FROM snmp_profiles WHERE id = ANY($1)`, []string{profExclusive, profShared}); got != 1 {
		t.Errorf("perfiles snmp tras la purga de A: %d, want 1 (el compartido con B)", got)
	}

	// Nada de A; B intacto (filas, reglas, ventana, chequeo, grupo).
	for _, id := range aIDs {
		if n := rowsOf(t, id); n != 0 {
			t.Errorf("quedan %d filas del host %s del grupo purgado", n, id)
		}
	}
	if n := rowsOf(t, x.hostB) + rowsOf(t, devB); n != beforeB {
		t.Errorf("la purga de A cambio las filas de B: %d, want %d", n, beforeB)
	}
	if got := x.count(`SELECT count(*) FROM alert_rules WHERE id = ANY($1)`, []string{ruleA, ruleB}); got != 1 {
		t.Errorf("reglas de A y B tras la purga: %d, want solo la de B", got)
	}
	if got := x.count(`SELECT count(*) FROM maintenance_windows WHERE id = ANY($1)`, []string{winA, winB}); got != 1 {
		t.Errorf("ventanas de A y B tras la purga: %d, want solo la de B", got)
	}
	if got := x.count(`SELECT count(*) FROM service_check_defs WHERE id = ANY($1)`, []string{chkA, chkB}); got != 1 {
		t.Errorf("chequeos de A y B tras la purga: %d, want solo el de B", got)
	}
	if got := x.count(`SELECT count(*) FROM host_groups WHERE id = ANY($1)`, []string{x.groupA, x.groupB}); got != 1 {
		t.Errorf("grupos A y B tras la purga: %d, want solo B", got)
	}
	if got := x.count(`SELECT count(*) FROM enroll_tokens WHERE group_id = $1`, x.groupA); got != 0 {
		t.Errorf("quedan %d enroll tokens del grupo purgado", got)
	}
	// El agente de A ya no entra; el host purgado no aparece en ninguna consulta.
	x.wantStatus(x.metrics(tokA), http.StatusUnauthorized, "invalid_token")
	for _, p := range []string{"/api/v1/hosts", "/api/v1/devices", "/api/v1/groups", "/api/v1/alerts", "/api/v1/summary"} {
		body := x.do("GET", p, "", withCookie(x.admin)).Body.String()
		for _, id := range aIDs {
			if strings.Contains(body, id) {
				t.Errorf("GET %s expone %s tras la purga", p, id)
			}
		}
	}
	// Viewers: el de solo A queda deshabilitado (sin sesion, sin login); el de A y B sigue y solo ve B.
	x.wantStatus(x.do("GET", "/api/v1/hosts", "", withCookie(x.va)), http.StatusUnauthorized, "unauthenticated")
	x.wantStatus(x.do("POST", "/api/v1/auth/login", loginBody("viewa", pwViewer)), http.StatusUnauthorized, "invalid_credentials")
	body := x.do("GET", "/api/v1/hosts", "", withCookie(vab)).Body.String()
	if !strings.Contains(body, x.hostB) || strings.Contains(body, x.hostA) {
		t.Errorf("el viewer de A y B tras la purga de A: %.300s", body)
	}
	if got := x.count(`SELECT count(*) FROM user_groups WHERE user_id = $1`, vabID); got != 1 {
		t.Errorf("grupos del viewer de A y B: %d, want 1", got)
	}
	// Auditoria con conteos y sin nombres.
	audit := x.do("GET", "/api/v1/audit?event=group_purged", "", withCookie(x.admin)).Body.String()
	if !strings.Contains(audit, "group_purged") || strings.Contains(audit, "host-a") || strings.Contains(audit, "col-a") {
		t.Errorf("auditoria de la purga de grupo: %.500s", audit)
	}
	// Repetirla: el grupo ya no existe.
	x.wantStatus(purge(x.admin, x.groupA, `{"confirm_name":"A"}`), http.StatusNotFound, "not_found")
	// Un admin sigue operando.
	x.wantStatus(x.do("GET", "/api/v1/hosts", "", withCookie(x.admin)), http.StatusOK, "")
}

// Sin delete_group ni disable_users: el grupo queda vacio y sus viewers sin acceso pero habilitados; un host que no se
// puede purgar (colector de un dispositivo de otro grupo, dato anterior a la regla) se reporta y no detiene al resto.
func TestPurgeGroupKeepsGroupAndReportsFailures(t *testing.T) {
	x := newScopeEnv(t)
	colX, _, _ := x.enrollHost(x.admin, "col-x")
	x.setGroupAPI(colX, x.groupA, http.StatusOK, "")
	devY := x.newDevice(fmt.Sprintf(`{"name":"sw-y","address":"10.1.0.9","collector_id":%q,"group_id":%q}`, colX, x.groupA))
	// Dato anterior a la regla: el dispositivo quedo en B con un colector de A.
	mustExec(t, `UPDATE hosts SET group_id = $2 WHERE id = $1`, devY, x.groupB)
	seedHostData(t, x.hostA)

	rec := x.do("POST", "/api/v1/groups/"+x.groupA+"/purge", `{"confirm_name":"A","delete_group":true}`, withCookie(x.admin))
	x.wantStatus(rec, http.StatusOK, "")
	var res struct {
		Agents       int      `json:"agents"`
		Failed       []string `json:"failed_host_ids"`
		GroupDeleted bool     `json:"group_deleted"`
	}
	x.dataAs(rec, &res)
	if res.Agents != 1 || len(res.Failed) != 1 || res.Failed[0] != colX || res.GroupDeleted {
		t.Errorf("purga con un fallo: %+v, want 1 agente borrado, fallo=%s y el grupo sin borrar", res, colX)
	}
	if n := rowsOf(t, x.hostA); n != 0 {
		t.Errorf("el host sano no se purgo: %d filas", n)
	}
	if rowsOf(t, devY) == 0 || rowsOf(t, colX) == 0 {
		t.Error("la purga de A toco el dispositivo de B o el colector que no se pudo purgar")
	}
	// Sin disable_users el viewer de A sigue habilitado pero ya no ve nada del grupo.
	if body := x.do("GET", "/api/v1/hosts", "", withCookie(x.va)).Body.String(); strings.Contains(body, x.hostA) || strings.Contains(body, colX) {
		t.Errorf("el viewer de A sigue viendo el grupo purgado: %.300s", body)
	}
	x.wantStatus(x.do("GET", "/api/v1/groups", "", withCookie(x.va)), http.StatusOK, "")

	// Al quitar el dispositivo de B, repetir la purga termina el trabajo (idempotente).
	x.wantStatus(x.do("DELETE", "/api/v1/devices/"+devY, "", withCookie(x.admin)), http.StatusOK, "")
	rec = x.do("POST", "/api/v1/groups/"+x.groupA+"/purge", `{"confirm_name":"A","delete_group":true}`, withCookie(x.admin))
	x.wantStatus(rec, http.StatusOK, "")
	x.dataAs(rec, &res)
	if res.Agents != 1 || len(res.Failed) != 0 || !res.GroupDeleted {
		t.Errorf("segunda purga: %+v, want 1 agente, sin fallos y grupo borrado", res)
	}
}

func TestPurgeRoutesAreAdminOnly(t *testing.T) {
	e := newEnv(t, Options{})
	for _, p := range []string{"POST /api/v1/hosts/{id}/purge", "POST /api/v1/groups/{id}/purge"} {
		found := false
		for _, rt := range e.api.routes {
			if rt.pattern == p {
				found = true
				if rt.access != accessAdmin {
					t.Errorf("%s: acceso %v, want accessAdmin", p, rt.access)
				}
			}
		}
		if !found {
			t.Errorf("ruta %s no registrada", p)
		}
	}
}
