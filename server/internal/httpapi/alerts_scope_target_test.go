package httpapi

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// Alcance host y dispositivo en reglas de alerta y ventanas de mantenimiento (migracion 00029): contrato, errores y ambito.

type scopeTargetEnv struct {
	*scopeEnv
	ae     *alertEnv
	device string // dispositivo del grupo A
}

func newScopeTargetEnv(t *testing.T) *scopeTargetEnv {
	t.Helper()
	s := newScopeEnv(t)
	x := &scopeTargetEnv{scopeEnv: s, ae: &alertEnv{env: s.env, admin: s.admin, viewer: s.va}}
	rec := s.do("POST", "/api/v1/devices", fmt.Sprintf(`{"name":"sw1","address":"10.60.0.1","collector_id":%q,"group_id":%q}`, s.hostA, s.groupA), withCookie(s.admin))
	s.wantStatus(rec, http.StatusCreated, "")
	var d struct {
		Device struct {
			ID string `json:"id"`
		} `json:"device"`
	}
	s.dataAs(rec, &d)
	x.device = d.Device.ID
	return x
}

type scopeOut struct {
	Type       string  `json:"type"`
	GroupID    *string `json:"group_id"`
	HostID     *string `json:"host_id"`
	HostName   *string `json:"host_name"`
	DeviceID   *string `json:"device_id"`
	DeviceName *string `json:"device_name"`
}

func TestRuleScopeHostAndDeviceContract(t *testing.T) {
	x := newScopeTargetEnv(t)
	post := func(body string) (int, scopeOut, string) {
		rec := x.do("POST", "/api/v1/alert-rules", body, withCookie(x.admin))
		var d struct {
			Rule struct {
				ID    string   `json:"id"`
				Scope scopeOut `json:"scope"`
			} `json:"rule"`
		}
		if rec.Code == http.StatusCreated {
			x.dataAs(rec, &d)
		}
		return rec.Code, d.Rule.Scope, d.Rule.ID
	}
	rule := func(kind, extra, scope string) string {
		return fmt.Sprintf(`{"name":"r-%d","kind":%q,%s"duration_seconds":0,"severity":"warn","scope":%s}`, time.Now().UnixNano(), kind, extra, scope)
	}
	hostSc := fmt.Sprintf(`{"type":"host","host_id":%q}`, x.hostA)
	devSc := fmt.Sprintf(`{"type":"device","device_id":%q}`, x.device)
	cpu := `"metric":"cpu_pct","op":"gt","threshold":90,`

	// Lectura del alcance host y device.
	code, sc, id := post(rule("metric", cpu, hostSc))
	if code != http.StatusCreated || sc.Type != "host" || sc.HostID == nil || *sc.HostID != x.hostA || sc.HostName == nil || *sc.HostName != "host-a" || sc.DeviceID != nil {
		t.Fatalf("regla metric host got %d %+v", code, sc)
	}
	code, sc, _ = post(rule("host_status", "", devSc))
	if code != http.StatusCreated || sc.Type != "device" || sc.DeviceID == nil || *sc.DeviceID != x.device || sc.DeviceName == nil || *sc.DeviceName != "sw1" || sc.HostID != nil {
		t.Fatalf("regla host_status device got %d %+v", code, sc)
	}
	if code, sc, _ = post(rule("host_status", "", hostSc)); code != http.StatusCreated || sc.Type != "host" {
		t.Errorf("host_status con host got %d %+v", code, sc)
	}

	// Combinaciones invalidas.
	bad := map[string]string{
		"device + metric":             rule("metric", cpu, devSc),
		"device + no_data":            rule("no_data", `"threshold":300,`, devSc),
		"device + exposed_port":       rule("exposed_port", "", devSc),
		"host + service_check":        rule("service_check", fmt.Sprintf(`"check_id":%q,`, missingID), hostSc),
		"device + service_check":      rule("service_check", fmt.Sprintf(`"check_id":%q,`, missingID), devSc),
		"host sin host_id":            rule("host_status", "", `{"type":"host"}`),
		"host con device_id":          rule("host_status", "", fmt.Sprintf(`{"type":"host","host_id":%q,"device_id":%q}`, x.hostA, x.device)),
		"host con group_id":           rule("host_status", "", fmt.Sprintf(`{"type":"host","host_id":%q,"group_id":%q}`, x.hostA, x.groupA)),
		"device con host_id":          rule("host_status", "", fmt.Sprintf(`{"type":"device","host_id":%q}`, x.hostA)),
		"device sin device_id":        rule("host_status", "", `{"type":"device"}`),
		"host_id malo":                rule("host_status", "", `{"type":"host","host_id":"x"}`),
		"all con host_id":             rule("host_status", "", fmt.Sprintf(`{"type":"all","host_id":%q}`, x.hostA)),
		"group con device_id":         rule("host_status", "", fmt.Sprintf(`{"type":"group","group_id":%q,"device_id":%q}`, x.groupA, x.device)),
		"tipo de alcance desconocido": rule("host_status", "", `{"type":"planeta"}`),
	}
	for _, body := range bad {
		x.wantStatus(x.do("POST", "/api/v1/alert-rules", body, withCookie(x.admin)), http.StatusBadRequest, "invalid_body")
	}
	// Destinos que no existen o de otro tipo: 422 con codigo propio.
	x.wantStatus(x.do("POST", "/api/v1/alert-rules", rule("host_status", "", fmt.Sprintf(`{"type":"host","host_id":%q}`, x.device)), withCookie(x.admin)), http.StatusUnprocessableEntity, "host_not_found")
	x.wantStatus(x.do("POST", "/api/v1/alert-rules", rule("host_status", "", fmt.Sprintf(`{"type":"host","host_id":%q}`, missingID)), withCookie(x.admin)), http.StatusUnprocessableEntity, "host_not_found")
	x.wantStatus(x.do("POST", "/api/v1/alert-rules", rule("host_status", "", fmt.Sprintf(`{"type":"device","device_id":%q}`, x.hostA)), withCookie(x.admin)), http.StatusUnprocessableEntity, "device_not_found")

	// PATCH: la regla metric de host no puede pasar a device; si a all, y de vuelta a host.
	x.wantStatus(x.do("PATCH", "/api/v1/alert-rules/"+id, fmt.Sprintf(`{"scope":{"type":"device","device_id":%q}}`, x.device), withCookie(x.admin)), http.StatusBadRequest, "invalid_body")
	rec := x.do("PATCH", "/api/v1/alert-rules/"+id, `{"scope":{"type":"all"}}`, withCookie(x.admin))
	x.wantStatus(rec, http.StatusOK, "")
	var got struct {
		Rule struct {
			Scope scopeOut `json:"scope"`
		} `json:"rule"`
	}
	x.dataAs(rec, &got)
	if got.Rule.Scope.Type != "all" || got.Rule.Scope.HostID != nil || got.Rule.Scope.HostName != nil {
		t.Errorf("host -> all got %+v", got.Rule.Scope)
	}
	rec = x.do("PATCH", "/api/v1/alert-rules/"+id, `{"scope":`+hostSc+`}`, withCookie(x.admin))
	x.wantStatus(rec, http.StatusOK, "")
	x.dataAs(rec, &got)
	if got.Rule.Scope.Type != "host" || got.Rule.Scope.HostName == nil || *got.Rule.Scope.HostName != "host-a" {
		t.Errorf("all -> host got %+v", got.Rule.Scope)
	}
}

func TestMaintenanceScopeHostAndDeviceContract(t *testing.T) {
	x := newScopeTargetEnv(t)
	now := x.clock.Now()
	post := func(scope string, dur time.Duration) (int, scopeOut, string) {
		rec := x.do("POST", "/api/v1/maintenance", withDates(`{"name":"m","scope":`+scope+`}`, now, dur), withCookie(x.admin))
		var d struct {
			Maintenance struct {
				ID    string   `json:"id"`
				Scope scopeOut `json:"scope"`
			} `json:"maintenance"`
		}
		if rec.Code == http.StatusCreated {
			x.dataAs(rec, &d)
		}
		return rec.Code, d.Maintenance.Scope, d.Maintenance.ID
	}
	hostSc := fmt.Sprintf(`{"type":"host","host_id":%q}`, x.hostA)
	devSc := fmt.Sprintf(`{"type":"device","device_id":%q}`, x.device)

	code, sc, id := post(hostSc, 72*time.Hour) // un host puede pasar de 24 h (el tope corto es solo de `all`)
	if code != http.StatusCreated || sc.Type != "host" || sc.HostName == nil || *sc.HostName != "host-a" {
		t.Fatalf("ventana host got %d %+v", code, sc)
	}
	if code, sc, _ = post(devSc, time.Hour); code != http.StatusCreated || sc.DeviceName == nil || *sc.DeviceName != "sw1" {
		t.Fatalf("ventana device got %d %+v", code, sc)
	}
	if code, _, _ = post(`{"type":"all"}`, 72*time.Hour); code != http.StatusBadRequest {
		t.Errorf("all de 72 h got %d, want 400", code)
	}
	if code, _, _ = post(`{"type":"device"}`, time.Hour); code != http.StatusBadRequest {
		t.Errorf("device sin id got %d, want 400", code)
	}
	if code, _, _ = post(fmt.Sprintf(`{"type":"host","host_id":%q}`, x.device), time.Hour); code != http.StatusUnprocessableEntity {
		t.Errorf("host que es un dispositivo got %d, want 422", code)
	}
	if code, _, _ = post(fmt.Sprintf(`{"type":"device","device_id":%q}`, x.hostA), time.Hour); code != http.StatusUnprocessableEntity {
		t.Errorf("dispositivo que es un agente got %d, want 422", code)
	}
	x.wantStatus(x.do("PATCH", "/api/v1/maintenance/"+id, `{"scope":`+devSc+`}`, withCookie(x.admin)), http.StatusOK, "")
}

func TestRuleAndWindowTargetScopeIsHiddenOutsideTheViewerScope(t *testing.T) {
	x := newScopeTargetEnv(t)
	own := x.ae.createRule(fmt.Sprintf(`{"name":"propia","kind":"host_status","duration_seconds":0,"severity":"warn","scope":{"type":"host","host_id":%q}}`, x.hostA))
	ownDev := x.ae.createRule(fmt.Sprintf(`{"name":"propia-dev","kind":"host_status","duration_seconds":0,"severity":"warn","scope":{"type":"device","device_id":%q}}`, x.device))
	foreign := x.ae.createRule(fmt.Sprintf(`{"name":"ajena","kind":"host_status","duration_seconds":0,"severity":"warn","scope":{"type":"host","host_id":%q}}`, x.hostB))
	loose := x.ae.createRule(fmt.Sprintf(`{"name":"sin-grupo","kind":"host_status","duration_seconds":0,"severity":"warn","scope":{"type":"host","host_id":%q}}`, x.hostNo))

	rec := x.do("GET", "/api/v1/alert-rules", "", withCookie(x.va))
	x.wantStatus(rec, http.StatusOK, "")
	var list struct {
		Items []struct{ ID string } `json:"items"`
	}
	x.dataAs(rec, &list)
	if len(list.Items) != 2 {
		t.Errorf("reglas visibles del viewer got %s, want solo propia y propia-dev", rec.Body.String())
	}
	for _, id := range []string{own, ownDev} {
		x.wantStatus(x.do("GET", "/api/v1/alert-rules/"+id, "", withCookie(x.va)), http.StatusOK, "")
	}
	for _, id := range []string{foreign, loose} {
		x.wantStatus(x.do("GET", "/api/v1/alert-rules/"+id, "", withCookie(x.va)), http.StatusNotFound, "not_found")
	}

	win := func(host string) string {
		return x.ae.createWindow(fmt.Sprintf(`{"name":"w","scope":{"type":"host","host_id":%q}}`, host), time.Hour)
	}
	wOwn, wForeign := win(x.hostA), win(x.hostB)
	x.wantStatus(x.do("GET", "/api/v1/maintenance/"+wOwn, "", withCookie(x.va)), http.StatusOK, "")
	x.wantStatus(x.do("GET", "/api/v1/maintenance/"+wForeign, "", withCookie(x.va)), http.StatusNotFound, "not_found")
}
