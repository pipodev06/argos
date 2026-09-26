package httpapi

import (
	"fmt"
	"net/http"
	"testing"
)

func TestRenameHost(t *testing.T) {
	x := newSNMPEnv(t)
	agent, _, _ := x.enrollHost(x.admin, "original")
	dev := x.newDevice(fmt.Sprintf(`{"name":"sw","address":"10.60.0.1","collector_id":%q}`, agent))
	url := func(id string) string { return "/api/v1/hosts/" + id + "/name" }

	x.wantStatus(x.do("PUT", url(agent), `{"hostname":"laptop-ana"}`, withCookie(x.admin)), http.StatusOK, "")
	x.wantStatus(x.do("PUT", url(agent), `{"hostname":""}`, withCookie(x.admin)), http.StatusBadRequest, "invalid_body")
	x.wantStatus(x.do("PUT", url(agent), `{"nombre":"x"}`, withCookie(x.admin)), http.StatusBadRequest, "invalid_body")
	x.wantStatus(x.do("PUT", url("no-uuid"), `{"hostname":"x"}`, withCookie(x.admin)), http.StatusBadRequest, "")
	// Un dispositivo se renombra con PATCH /devices/{id}, no con este endpoint.
	x.wantStatus(x.do("PUT", url(dev), `{"hostname":"x"}`, withCookie(x.admin)), http.StatusNotFound, "not_found")
	// Solo admin.
	x.wantStatus(x.do("PUT", url(agent), `{"hostname":"otro"}`, withCookie(x.va)), http.StatusForbidden, "")

	rec := x.do("GET", "/api/v1/hosts/"+agent, "", withCookie(x.admin))
	x.wantStatus(rec, http.StatusOK, "")
	var d struct {
		Host struct {
			Hostname string `json:"hostname"`
		} `json:"host"`
	}
	x.dataAs(rec, &d)
	if d.Host.Hostname != "laptop-ana" {
		t.Errorf("hostname = %q, want laptop-ana", d.Host.Hostname)
	}
}
