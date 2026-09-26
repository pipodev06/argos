package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestServiceCheckHostScopeAPI(t *testing.T) {
	x := newScopeEnv(t)
	dev := x.newDevice(fmt.Sprintf(`{"name":"dev","address":"10.9.0.1","collector_id":%q,"group_id":%q}`, x.hostA, x.groupA))
	post := func(body string, status int, code string) {
		t.Helper()
		x.wantStatus(x.do("POST", "/api/v1/service-checks", body, withCookie(x.admin)), status, code)
	}
	tcp := func(name, scope string) string {
		return fmt.Sprintf(`{"name":%q,"type":"tcp","target_host":"localhost","target_port":22,"scope":%s}`, name, scope)
	}

	rec := x.do("POST", "/api/v1/service-checks", tcp("solo-b", fmt.Sprintf(`{"type":"host","host_id":%q}`, x.hostB)), withCookie(x.admin))
	x.wantStatus(rec, http.StatusCreated, "")
	var got struct {
		Check struct {
			ID    string `json:"id"`
			Scope struct {
				Type     string  `json:"type"`
				HostID   *string `json:"host_id"`
				HostName *string `json:"host_name"`
				DevID    *string `json:"device_id"`
			} `json:"scope"`
		} `json:"check"`
	}
	x.dataAs(rec, &got)
	sc := got.Check.Scope
	if sc.Type != "host" || sc.HostID == nil || *sc.HostID != x.hostB || sc.HostName == nil || *sc.HostName != "host-b" || sc.DevID != nil {
		t.Fatalf("scope leido got %+v, want host host-b sin device_id", sc)
	}

	// Errores: dispositivo, inexistente, tipos incompatibles y forma del alcance.
	post(tcp("a", fmt.Sprintf(`{"type":"host","host_id":%q}`, dev)), http.StatusUnprocessableEntity, "host_not_found")
	post(tcp("b", fmt.Sprintf(`{"type":"host","host_id":%q}`, missingID)), http.StatusUnprocessableEntity, "host_not_found")
	post(fmt.Sprintf(`{"name":"c","type":"ping","scope":{"type":"host","host_id":%q}}`, x.hostB), http.StatusBadRequest, "invalid_body")
	post(fmt.Sprintf(`{"name":"d","type":"snmp","metric":"reachable","scope":{"type":"host","host_id":%q}}`, x.hostB), http.StatusBadRequest, "invalid_body")
	post(tcp("e", fmt.Sprintf(`{"type":"device","device_id":%q}`, dev)), http.StatusBadRequest, "invalid_body")
	post(tcp("f", `{"type":"host"}`), http.StatusBadRequest, "invalid_body")
	post(tcp("g", fmt.Sprintf(`{"type":"host","host_id":%q,"group_id":%q}`, x.hostB, x.groupA)), http.StatusBadRequest, "invalid_body")
	post(tcp("h", fmt.Sprintf(`{"type":"all","host_id":%q}`, x.hostB)), http.StatusBadRequest, "invalid_body")

	// Solo el host asignado lo recibe en su lista de chequeos.
	if body := x.do("GET", "/api/v1/hosts/"+x.hostB+"/service-checks", "", withCookie(x.admin)).Body.String(); !strings.Contains(body, "solo-b") {
		t.Errorf("host-b no ve su chequeo: %.300s", body)
	}
	if body := x.do("GET", "/api/v1/hosts/"+x.hostA+"/service-checks", "", withCookie(x.admin)).Body.String(); strings.Contains(body, "solo-b") {
		t.Errorf("host-a ve el chequeo de host-b: %.300s", body)
	}

	// PATCH: mover a otro host y volver a all.
	url := "/api/v1/service-checks/" + got.Check.ID
	rec = x.do("PATCH", url, fmt.Sprintf(`{"scope":{"type":"host","host_id":%q}}`, x.hostA), withCookie(x.admin))
	x.wantStatus(rec, http.StatusOK, "")
	if !strings.Contains(rec.Body.String(), `"host_name":"host-a"`) {
		t.Errorf("PATCH a host-a got %.300s", rec.Body.String())
	}
	x.wantStatus(x.do("PATCH", url, fmt.Sprintf(`{"scope":{"type":"host","host_id":%q}}`, dev), withCookie(x.admin)), http.StatusUnprocessableEntity, "host_not_found")
	rec = x.do("PATCH", url, `{"scope":{"type":"all"}}`, withCookie(x.admin))
	x.wantStatus(rec, http.StatusOK, "")
	if strings.Contains(rec.Body.String(), "host_id") {
		t.Errorf("scope all no debe llevar host_id: %.300s", rec.Body.String())
	}
}
