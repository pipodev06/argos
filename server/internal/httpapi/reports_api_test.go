package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"argos/server/internal/store"
)

type availabilityResp struct {
	Data struct {
		Range string `json:"range"`
		By    string `json:"by"`
		Fleet struct {
			Hosts int `json:"hosts"`
		} `json:"fleet"`
		Items []struct {
			HostID    string  `json:"host_id"`
			Hostname  string  `json:"hostname"`
			GroupID   *string `json:"group_id"`
			GroupName *string `json:"group_name"`
			Hosts     int     `json:"hosts"`
		} `json:"items"`
	} `json:"data"`
}

func (x *scopeEnv) availability(c *http.Cookie, query string) (*httptest.ResponseRecorder, availabilityResp) {
	x.t.Helper()
	rec := x.do("GET", "/api/v1/reports/availability"+query, "", withCookie(c))
	var out availabilityResp
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			x.t.Fatalf("respuesta invalida: %v (%s)", err, rec.Body.String())
		}
	}
	return rec, out
}

// TestReportFreeRange fija el contrato de B12 (rango libre y mes calendario) en parseReportParams,
// compartido por los 5 informes: from+to, month, y las combinaciones invalidas.
func TestReportFreeRange(t *testing.T) {
	x := newScopeEnv(t)

	rec, resp := x.availability(x.admin, "?from=2026-08-01T00:00:00Z&to=2026-08-08T00:00:00Z")
	if rec.Code != http.StatusOK || resp.Data.Range != "custom" {
		t.Fatalf("from/to got %d %+v (%s)", rec.Code, resp, rec.Body.String())
	}

	rec, resp = x.availability(x.admin, "?month=2026-08")
	if rec.Code != http.StatusOK || resp.Data.Range != "2026-08" {
		t.Fatalf("month got %d %+v (%s)", rec.Code, resp, rec.Body.String())
	}

	// "hasta" en el futuro se recorta a ahora en vez de rechazarse.
	if rec, _ := x.availability(x.admin, "?from="+url.QueryEscape(time.Now().Add(-48*time.Hour).UTC().Format(time.RFC3339))+"&to=2099-01-01T00:00:00Z"); rec.Code != http.StatusOK {
		t.Errorf("to futuro: got %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	for name, query := range map[string]string{
		"from sin to":              "?from=2026-08-01T00:00:00Z",
		"to sin from":              "?to=2026-08-08T00:00:00Z",
		"from despues de to":       "?from=2026-08-08T00:00:00Z&to=2026-08-01T00:00:00Z",
		"rango mayor a 91 dias":    "?from=2026-01-01T00:00:00Z&to=2026-08-01T00:00:00Z",
		"month mas range":          "?month=2026-08&range=7d",
		"month mas from":           "?month=2026-08&from=2026-08-01T00:00:00Z&to=2026-08-08T00:00:00Z",
		"from/to mas range":        "?from=2026-08-01T00:00:00Z&to=2026-08-08T00:00:00Z&range=7d",
		"mes con formato invalido": "?month=agosto-2026",
		"mes futuro":               "?month=2099-01",
	} {
		t.Run(name, func(t *testing.T) {
			rec, _ := x.availability(x.admin, query)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s: got %d, want 400 (%s)", name, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAvailabilityReportScope(t *testing.T) {
	x := newScopeEnv(t)

	// Admin ve los 3 hosts; el viewer restringido a A solo el suyo, ni en la lista ni en el total de la flota.
	rec, adm := x.availability(x.admin, "")
	if rec.Code != http.StatusOK || len(adm.Data.Items) != 3 || adm.Data.Fleet.Hosts != 3 || adm.Data.Range != "30d" || adm.Data.By != "host" {
		t.Fatalf("admin got %d %s", rec.Code, rec.Body.String())
	}
	rec, va := x.availability(x.va, "")
	if rec.Code != http.StatusOK || len(va.Data.Items) != 1 || va.Data.Items[0].HostID != x.hostA || va.Data.Fleet.Hosts != 1 {
		t.Fatalf("viewer restringido got %d %s", rec.Code, rec.Body.String())
	}
	for _, leak := range []string{x.hostB, x.hostNo, x.groupB, "host-b", "host-none"} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Errorf("la respuesta del viewer contiene dato ajeno %q", leak)
		}
	}
	// Por grupo: el viewer solo ve A; el admin ve A, B y "sin grupo".
	_, vg := x.availability(x.va, "?by=group&range=7d")
	if len(vg.Data.Items) != 1 || vg.Data.Items[0].GroupID == nil || *vg.Data.Items[0].GroupID != x.groupA || vg.Data.Items[0].Hosts != 1 {
		t.Errorf("by=group viewer got %+v", vg.Data.Items)
	}
	_, ag := x.availability(x.admin, "?by=group")
	if len(ag.Data.Items) != 3 || ag.Data.Items[2].GroupID != nil {
		t.Errorf("by=group admin got %+v, want A, B y sin grupo al final", ag.Data.Items)
	}
	// group_id propio: ok; ajeno e inexistente: 404 identico (incluido un id mal formado).
	if rec, _ := x.availability(x.va, "?group_id="+x.groupA); rec.Code != http.StatusOK {
		t.Errorf("group_id propio got %d", rec.Code)
	}
	missing, _ := x.availability(x.va, "?group_id="+missingID)
	for name, q := range map[string]string{"ajeno": "?group_id=" + x.groupB, "malformado": "?group_id=xx"} {
		rec, _ := x.availability(x.va, q)
		if rec.Code != http.StatusNotFound || rec.Body.String() != missing.Body.String() {
			t.Errorf("group_id %s got %d %q, want 404 identico al inexistente (%q)", name, rec.Code, rec.Body.String(), missing.Body.String())
		}
	}
	if missing.Code != http.StatusNotFound {
		t.Errorf("group_id inexistente got %d, want 404", missing.Code)
	}
	// El admin si puede filtrar por B.
	if rec, r := x.availability(x.admin, "?group_id="+x.groupB); rec.Code != http.StatusOK || len(r.Data.Items) != 1 || r.Data.Items[0].HostID != x.hostB {
		t.Errorf("admin group_id B got %d %s", rec.Code, rec.Body.String())
	}
	// Parametros invalidos.
	for _, q := range []string{"?range=1d", "?by=x"} {
		if rec, _ := x.availability(x.admin, q); rec.Code != http.StatusBadRequest {
			t.Errorf("%s got %d, want 400", q, rec.Code)
		}
	}
	// Sin sesion: 401.
	if rec := x.do("GET", "/api/v1/reports/availability", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("sin sesion got %d, want 401", rec.Code)
	}
}

func TestServiceChecksReportScope(t *testing.T) {
	x := newScopeEnv(t)
	for _, h := range []string{x.hostA, x.hostB} {
		mustExec(t, `INSERT INTO service_check_defs (id, name, type, target_host, target_port) VALUES (gen_random_uuid(), $1, 'tcp', 'localhost', 22)`, "web-"+h[:8])
		mustExec(t, `INSERT INTO service_check_events (host_id, check_id, from_status, to_status, at)
			SELECT $1, id, NULL, 'up', now() - interval '1 hour' FROM service_check_defs WHERE name = $2`, h, "web-"+h[:8])
	}
	rec := x.do("GET", "/api/v1/reports/service-checks?range=7d", "", withCookie(x.admin))
	if rec.Code != http.StatusOK || strings.Count(rec.Body.String(), `"check_id"`) != 2 {
		t.Fatalf("admin got %d %s", rec.Code, rec.Body.String())
	}
	rec = x.do("GET", "/api/v1/reports/service-checks", "", withCookie(x.va))
	if rec.Code != http.StatusOK || strings.Count(rec.Body.String(), `"check_id"`) != 1 || strings.Contains(rec.Body.String(), x.hostB) {
		t.Errorf("viewer restringido got %d %s", rec.Code, rec.Body.String())
	}
	if rec := x.do("GET", "/api/v1/reports/service-checks?group_id="+x.groupB, "", withCookie(x.va)); rec.Code != http.StatusNotFound {
		t.Errorf("group_id ajeno got %d, want 404", rec.Code)
	}
}

func TestReportConcurrencyLimitAndErrorMapping(t *testing.T) {
	x := newScopeEnv(t)
	// Con los cupos ocupados, el siguiente informe recibe 429 con Retry-After; al liberar, vuelve a funcionar.
	for range maxConcurrentReports {
		x.api.reportSem <- struct{}{}
	}
	rec := x.do("GET", "/api/v1/reports/availability", "", withCookie(x.admin))
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Errorf("con cupos llenos got %d (Retry-After %q), want 429", rec.Code, rec.Header().Get("Retry-After"))
	}
	for range maxConcurrentReports {
		<-x.api.reportSem
	}
	if rec := x.do("GET", "/api/v1/reports/availability", "", withCookie(x.admin)); rec.Code != http.StatusOK {
		t.Errorf("con cupos libres got %d, want 200", rec.Code)
	}
	if len(x.api.reportSem) != 0 {
		t.Errorf("el cupo no se libero: %d ocupados", len(x.api.reportSem))
	}

	// Traduccion de errores: demasiados hosts = 422 narrow_with_group_id; timeout = 503 sin detalle interno.
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{store.ErrReportTooLarge, http.StatusUnprocessableEntity, "narrow_with_group_id"},
		{store.ErrReportTimeout, http.StatusServiceUnavailable, "report_timeout"},
	} {
		req := httptest.NewRequest("GET", "/x", nil)
		w := httptest.NewRecorder()
		x.api.handle(func(http.ResponseWriter, *http.Request) error { return x.api.reportErr(req, tc.err) })(w, req)
		x.wantStatus(w, tc.status, tc.code)
	}
}
