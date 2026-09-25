package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"argos/server/internal/notify"
	"argos/server/internal/store"
)

// Perfiles SNMP y chequeos snmp contra Postgres real: CRUD de admin, secretos que nunca salen, entrega al colector
// y resultados.

const (
	snmpAuthPass  = "auth-pass-ULTRA-SECRETA"
	snmpPrivPass  = "priv-pass-ULTRA-SECRETA"
	snmpCommunity = "comunidad-ULTRA-SECRETA"
	snmpAuthPass2 = "auth-pass-ROTADA-2"
)

func newSNMPEnv(t *testing.T) *scopeEnv {
	t.Helper()
	box, err := notify.NewBox(channelKey)
	if err != nil {
		t.Fatal(err)
	}
	return newScopeEnvWith(t, Options{HostDownAfter: 2 * time.Minute, Secrets: box})
}

func v3Body(name string) string {
	return fmt.Sprintf(`{"name":%q,"version":3,"username":"monitor","auth_proto":"SHA256","auth_pass":%q,"priv_proto":"AES128","priv_pass":%q}`,
		name, snmpAuthPass, snmpPrivPass)
}

func (s *scopeEnv) newProfile(body string) string {
	s.t.Helper()
	rec := s.do("POST", "/api/v1/snmp-profiles", body, withCookie(s.admin))
	s.wantStatus(rec, http.StatusCreated, "")
	var d struct {
		Profile struct {
			ID string `json:"id"`
		} `json:"profile"`
	}
	s.dataAs(rec, &d)
	return d.Profile.ID
}

// noSecrets falla si el texto contiene alguna contrasena o la comunidad.
func noSecrets(t *testing.T, what, text string) {
	t.Helper()
	for _, sec := range []string{snmpAuthPass, snmpPrivPass, snmpCommunity, snmpAuthPass2} {
		if strings.Contains(text, sec) {
			t.Errorf("%s contiene un secreto snmp: %.300s", what, text)
		}
	}
}

func TestSNMPProfilesCRUDAndValidation(t *testing.T) {
	x := newSNMPEnv(t)
	post := func(body string, status int, code string) {
		t.Helper()
		x.wantStatus(x.do("POST", "/api/v1/snmp-profiles", body, withCookie(x.admin)), status, code)
	}

	id := x.newProfile(v3Body("core"))
	rec := x.do("GET", "/api/v1/snmp-profiles/"+id, "", withCookie(x.admin))
	x.wantStatus(rec, http.StatusOK, "")
	noSecrets(t, "GET perfil", rec.Body.String())
	var got struct {
		Profile map[string]any `json:"profile"`
	}
	x.dataAs(rec, &got)
	p := got.Profile
	if p["version"] != float64(3) || p["level"] != "authPriv" || p["auth_proto"] != "SHA256" || p["priv_proto"] != "AES128" ||
		p["username"] != "monitor" || p["has_secret"] != true || p["has_community"] != false || p["allow_v2c"] != false {
		t.Errorf("perfil v3 got %v", p)
	}

	bad := []string{
		`{"name":"x","version":1,"community":"public"}`,
		`{"name":"x"}`,
		`{"name":"","version":3}`,
		`{"name":"x","version":3,"username":"u","auth_proto":"MD5","auth_pass":"12345678","priv_proto":"AES128","priv_pass":"12345678"}`,
		`{"name":"x","version":3,"username":"u","auth_proto":"SHA1","auth_pass":"12345678","priv_proto":"AES128","priv_pass":"12345678"}`,
		`{"name":"x","version":3,"username":"u","auth_proto":"SHA256","auth_pass":"12345678","priv_proto":"DES","priv_pass":"12345678"}`,
		`{"name":"x","version":3,"username":"u","auth_proto":"SHA256","auth_pass":"corta","priv_proto":"AES128","priv_pass":"12345678"}`,
		`{"name":"x","version":3,"username":"u v","auth_proto":"SHA256","auth_pass":"12345678","priv_proto":"AES128","priv_pass":"12345678"}`,
		`{"name":"x","version":3,"username":"u","auth_proto":"SHA256","auth_pass":"12345678"}`,
		`{"name":"x","version":3,"username":"u","auth_proto":"SHA256","auth_pass":"12345678","priv_proto":"AES128","priv_pass":"12345678","community":"c"}`,
		`{"name":"x","version":2,"community":"public"}`,
		`{"name":"x","version":2,"community":"public","allow_v2c":false}`,
		`{"name":"x","version":2,"allow_v2c":true}`,
		`{"name":"x","version":2,"allow_v2c":true,"community":"public","username":"u"}`,
		`{"name":"x","version":3,"extra":1}`,
	}
	for _, b := range bad {
		post(b, http.StatusBadRequest, "invalid_body")
	}
	post(v3Body("CORE"), http.StatusConflict, "profile_exists") // sin distinguir mayusculas

	// v2c solo con confirmacion explicita; nunca devuelve la comunidad.
	v2 := x.do("POST", "/api/v1/snmp-profiles",
		fmt.Sprintf(`{"name":"viejo","version":2,"allow_v2c":true,"community":%q}`, snmpCommunity), withCookie(x.admin))
	x.wantStatus(v2, http.StatusCreated, "")
	noSecrets(t, "crear v2c", v2.Body.String())
	if !strings.Contains(v2.Body.String(), `"has_community":true`) || !strings.Contains(v2.Body.String(), `"allow_v2c":true`) {
		t.Errorf("perfil v2c got %s", v2.Body.String())
	}

	// PATCH: cambiar una contrasena, campos de la otra version y version inmutable.
	patch := func(pid, body string, status int, code string) {
		t.Helper()
		x.wantStatus(x.do("PATCH", "/api/v1/snmp-profiles/"+pid, body, withCookie(x.admin)), status, code)
	}
	patch(id, fmt.Sprintf(`{"auth_pass":%q}`, snmpAuthPass2), http.StatusOK, "")
	patch(id, `{"community":"public"}`, http.StatusBadRequest, "invalid_body")
	patch(id, `{"version":2}`, http.StatusBadRequest, "invalid_body")
	patch(id, `{"priv_proto":"AES256","auth_proto":"SHA512"}`, http.StatusOK, "")
	patch(id, `{"auth_proto":"MD5"}`, http.StatusBadRequest, "invalid_body")
	patch(id, `{"auth_pass":"corta"}`, http.StatusBadRequest, "invalid_body")
	patch(id, `{}`, http.StatusBadRequest, "invalid_body")
	patch(missingID, `{"name":"z"}`, http.StatusNotFound, "not_found")

	// Un perfil en uso no se borra; la lectura y las escrituras son solo admin.
	col, _, _ := x.enrollHost(x.admin, "colector")
	dev := x.newDevice(fmt.Sprintf(`{"name":"sw","address":"10.30.0.1","collector_id":%q}`, col))
	x.wantStatus(x.do("PATCH", "/api/v1/devices/"+dev, fmt.Sprintf(`{"snmp_profile_id":%q}`, id), withCookie(x.admin)), http.StatusOK, "")
	x.wantStatus(x.do("DELETE", "/api/v1/snmp-profiles/"+id, "", withCookie(x.admin)), http.StatusConflict, "profile_in_use")
	x.wantStatus(x.do("PATCH", "/api/v1/devices/"+dev, `{"snmp_profile_id":null}`, withCookie(x.admin)), http.StatusOK, "")
	x.wantStatus(x.do("DELETE", "/api/v1/snmp-profiles/"+id, "", withCookie(x.admin)), http.StatusOK, "")
	x.wantStatus(x.do("GET", "/api/v1/snmp-profiles/"+id, "", withCookie(x.admin)), http.StatusNotFound, "not_found")
	x.wantStatus(x.do("PATCH", "/api/v1/devices/"+dev, fmt.Sprintf(`{"snmp_profile_id":%q}`, missingID), withCookie(x.admin)),
		http.StatusUnprocessableEntity, "profile_not_found")
	x.wantStatus(x.do("PATCH", "/api/v1/devices/"+dev, `{"snmp_profile_id":"no-uuid"}`, withCookie(x.admin)), http.StatusBadRequest, "invalid_body")
	for _, c := range []*http.Cookie{x.va, x.vall} {
		x.wantStatus(x.do("GET", "/api/v1/snmp-profiles", "", withCookie(c)), http.StatusForbidden, "")
		x.wantStatus(x.do("POST", "/api/v1/snmp-profiles", v3Body("v"), withCookie(c)), http.StatusForbidden, "")
	}

	// Ni la lista ni la auditoria llevan secretos, y el blob cifrado no contiene el texto plano.
	noSecrets(t, "lista de perfiles", x.do("GET", "/api/v1/snmp-profiles", "", withCookie(x.admin)).Body.String())
	audit := x.do("GET", "/api/v1/audit?limit=100", "", withCookie(x.admin)).Body.String()
	noSecrets(t, "auditoria", audit)
	if !strings.Contains(audit, "snmp_profile_created") || !strings.Contains(audit, "snmp_profile_updated") {
		t.Errorf("la auditoria no registra los cambios de perfil: %.300s", audit)
	}
	if n := x.count(`SELECT count(*) FROM snmp_profiles WHERE position(convert_to($1::text, 'UTF8') in secret_enc) > 0`, snmpCommunity); n != 0 {
		t.Errorf("la comunidad esta en claro en la BD")
	}
}

func TestSNMPChecksEndToEndThroughCollector(t *testing.T) {
	x := newSNMPEnv(t)
	col, tok, _ := x.enrollHost(x.admin, "colector")
	prof := x.newProfile(v3Body("core"))
	dev := x.newDevice(fmt.Sprintf(`{"name":"switch","address":"10.40.0.5","collector_id":%q}`, col))
	noProf := x.newDevice(fmt.Sprintf(`{"name":"sin-perfil","address":"10.40.0.6","collector_id":%q}`, col))
	x.wantStatus(x.do("PATCH", "/api/v1/devices/"+dev, fmt.Sprintf(`{"snmp_profile_id":%q}`, prof), withCookie(x.admin)), http.StatusOK, "")

	create := func(body string, status int, code string) string {
		t.Helper()
		rec := x.do("POST", "/api/v1/service-checks", body, withCookie(x.admin))
		x.wantStatus(rec, status, code)
		var ck struct {
			Check struct {
				ID string `json:"id"`
			} `json:"check"`
		}
		if status == http.StatusCreated {
			x.dataAs(rec, &ck)
		}
		return ck.Check.ID
	}
	reach := create(`{"name":"snmp-up","type":"snmp","metric":"reachable"}`, http.StatusCreated, "")
	oper := create(fmt.Sprintf(`{"name":"snmp-if3","type":"snmp","metric":"if_oper","if_index":3,"scope":{"type":"device","device_id":%q}}`, dev), http.StatusCreated, "")
	for _, b := range []string{
		`{"name":"a","type":"snmp"}`,
		`{"name":"a","type":"snmp","metric":"cpu"}`,
		`{"name":"a","type":"snmp","metric":"reachable","if_index":1}`,
		`{"name":"a","type":"snmp","metric":"if_oper"}`,
		`{"name":"a","type":"snmp","metric":"if_oper","if_index":0}`,
		`{"name":"a","type":"snmp","metric":"if_oper","if_index":65536}`,
		`{"name":"a","type":"snmp","metric":"reachable","target_host":"10.0.0.1"}`,
		`{"name":"a","type":"snmp","metric":"reachable","warn_ms":10}`,
		`{"name":"a","type":"tcp","target_host":"h","target_port":22,"metric":"reachable"}`,
	} {
		create(b, http.StatusBadRequest, "invalid_body")
	}

	// Entrega: un item por dispositivo con perfil (no el que no tiene), con IP literal, id determinista y secretos.
	x.clock.Advance(11 * time.Second)
	r := x.do("POST", "/api/v1/agent/metrics", `{"metrics":{"cpu_pct":1},"checks_rev":"","services":[]}`, withHeader("Authorization", "Bearer "+tok))
	x.wantStatus(r, http.StatusOK, "")
	var resp reportResp
	x.dataAs(r, &resp)
	if resp.Checks == nil || len(resp.Checks.Items) != 2 {
		t.Fatalf("entrega al colector got %+v, want 2 items snmp", resp.Checks)
	}
	byID := map[string]map[string]any{}
	for _, it := range resp.Checks.Items {
		byID[it["id"].(string)] = it
	}
	reachID, operID := store.SNMPItemID(reach, dev), store.SNMPItemID(oper, dev)
	ir, io := byID[reachID], byID[operID]
	if ir == nil || io == nil {
		t.Fatalf("faltan items: got %v, want ids %s y %s", byID, reachID, operID)
	}
	if ir["type"] != "snmp" || ir["metric"] != "reachable" || ir["target"] != "10.40.0.5" || ir["device_id"] != dev {
		t.Errorf("item reachable got %v", ir)
	}
	if _, has := ir["if_index"]; has {
		t.Error("reachable no lleva if_index")
	}
	if io["metric"] != "if_oper" || io["if_index"] != float64(3) {
		t.Errorf("item if_oper got %v", io)
	}
	sn, _ := ir["snmp"].(map[string]any)
	want := map[string]any{"version": float64(3), "user": "monitor", "level": "authPriv", "auth_proto": "SHA256", "auth_pass": snmpAuthPass,
		"priv_proto": "AES128", "priv_pass": snmpPrivPass}
	for k, v := range want {
		if sn[k] != v {
			t.Errorf("snmp.%s got %v, want %v", k, sn[k], v)
		}
	}
	if _, has := sn["community"]; has {
		t.Error("un perfil v3 no lleva community")
	}
	if _, has := ir["target_host"]; has {
		t.Error("un chequeo snmp no lleva target_host")
	}
	rev1 := resp.Checks.Rev

	// Rotar la contrasena cambia la revision (el colector recibe la nueva) sin que la revision la contenga.
	x.wantStatus(x.do("PATCH", "/api/v1/snmp-profiles/"+prof, fmt.Sprintf(`{"auth_pass":%q}`, snmpAuthPass2), withCookie(x.admin)), http.StatusOK, "")
	x.clock.Advance(11 * time.Second)
	r = x.do("POST", "/api/v1/agent/metrics", fmt.Sprintf(`{"metrics":{"cpu_pct":1},"checks_rev":%q}`, rev1), withHeader("Authorization", "Bearer "+tok))
	var resp2 reportResp
	x.dataAs(r, &resp2)
	if resp2.Checks == nil || resp2.Checks.Rev == rev1 || !strings.Contains(r.Body.String(), snmpAuthPass2) {
		t.Errorf("tras rotar la contrasena se esperaba una revision nueva con la contrasena nueva: %.300s", r.Body.String())
	}

	// Un perfil v2c se entrega marcado version 2 y solo con la comunidad.
	v2 := x.newProfile(fmt.Sprintf(`{"name":"viejo","version":2,"allow_v2c":true,"community":%q}`, snmpCommunity))
	x.wantStatus(x.do("PATCH", "/api/v1/devices/"+dev, fmt.Sprintf(`{"snmp_profile_id":%q}`, v2), withCookie(x.admin)), http.StatusOK, "")
	x.clock.Advance(11 * time.Second)
	r = x.do("POST", "/api/v1/agent/metrics", `{"metrics":{"cpu_pct":1},"checks_rev":""}`, withHeader("Authorization", "Bearer "+tok))
	var resp3 reportResp
	x.dataAs(r, &resp3)
	found := false
	for _, it := range resp3.Checks.Items {
		sn, _ := it["snmp"].(map[string]any)
		if sn["version"] != float64(2) || sn["community"] != snmpCommunity || sn["auth_pass"] != nil || sn["user"] != nil {
			t.Errorf("item v2c got %v", sn)
		}
		found = true
	}
	if !found {
		t.Error("el colector no recibio items v2c")
	}

	// Resultados: if_oper down no tumba el dispositivo; reachable up lo pone up; el catalogo acepta los detail nuevos.
	send := func(check, status, detail string) {
		t.Helper()
		x.clock.Advance(11 * time.Second)
		body := fmt.Sprintf(`{"metrics":{"cpu_pct":1},"services":[{"check_id":%q,"name":"","status":%q,"response_ms":3,"value":2,"detail":%q,"ts":%d}]}`,
			check, status, detail, x.clock.Now().Unix())
		rec := x.do("POST", "/api/v1/agent/metrics", body, withHeader("Authorization", "Bearer "+tok))
		if detail == "bogus" {
			x.wantStatus(rec, http.StatusBadRequest, "invalid_body")
		} else {
			x.wantStatus(rec, http.StatusOK, "")
		}
	}
	devStatus := func() string {
		var d struct {
			Device deviceView `json:"device"`
		}
		x.dataAs(x.do("GET", "/api/v1/devices/"+dev, "", withCookie(x.admin)), &d)
		return d.Device.Status
	}
	send(operID, "down", "ok")
	if s := devStatus(); s != "pending" {
		t.Errorf("una interfaz caida cambio el estado del dispositivo a %q, want pending", s)
	}
	send(reachID, "down", "auth_failed")
	send(reachID, "warn", "no_such_object")
	send(reachID, "up", "bogus")
	send(reachID, "up", "ok")
	if s := devStatus(); s != "up" {
		t.Errorf("estado del dispositivo tras reachable up got %q, want up", s)
	}
	if n := x.count(`SELECT count(*) FROM service_check_state WHERE host_id = $1 AND check_id = $2`, dev, oper); n != 1 {
		t.Errorf("el estado if_oper se guarda sobre el dispositivo: got %d filas, want 1", n)
	}
	if n := x.count(`SELECT count(*) FROM service_check_state WHERE host_id = $1`, noProf); n != 0 {
		t.Errorf("un dispositivo sin perfil tiene estado: %d filas", n)
	}
	rec := x.do("GET", "/api/v1/hosts/"+dev+"/service-checks", "", withCookie(x.admin))
	if !strings.Contains(rec.Body.String(), `"metric":"if_oper"`) || !strings.Contains(rec.Body.String(), `"if_index":3`) {
		t.Errorf("chequeos del dispositivo sin metric/if_index: %.400s", rec.Body.String())
	}

	// Ninguna lectura de admin ni la auditoria llevan secretos.
	for _, path := range []string{"/api/v1/devices/" + dev, "/api/v1/service-checks", "/api/v1/service-checks/" + oper,
		"/api/v1/audit?limit=100", "/api/v1/hosts/" + dev + "/service-checks"} {
		noSecrets(t, "GET "+path, x.do("GET", path, "", withCookie(x.admin)).Body.String())
	}
	// Un viewer restringido no ve el perfil del dispositivo (dato de admin).
	if body := x.do("GET", "/api/v1/devices", "", withCookie(x.vall)).Body.String(); strings.Contains(body, "snmp_profile") {
		t.Errorf("un viewer ve el perfil snmp: %.300s", body)
	}
}
