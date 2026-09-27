package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"
)

// Permisos por grupo (tanda 3): guard de inventario y enumeracion de todas las rutas GET.

// TestRequireInventoryGuard prueba el guard con una ruta que existe SOLO aqui (no hay ruta publica de inventario).
func TestRequireInventoryGuard(t *testing.T) {
	x := newAlertScopeEnv(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /test/hosts/{id}/inventory", x.api.handle(x.api.requireSession(false, x.api.requireInventory(
		func(w http.ResponseWriter, _ *http.Request) error { w.WriteHeader(http.StatusOK); return nil }))))
	get := func(c *http.Cookie, hostID string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/test/hosts/"+hostID+"/inventory", nil)
		req.Host = "localhost:8080"
		req.AddCookie(c)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	x.patchUser(x.vaID, `{"all_groups":false,"group_ids":["`+x.groupA+`"],"can_inventory":false}`, http.StatusOK, "")

	// Sin can_inventory: 404 en todos los hosts, incluso el propio y para un viewer con ambito total.
	missing := get(x.va, missingID)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("host inexistente got %d, want 404", missing.Code)
	}
	for _, h := range []string{x.hostA, x.hostB, x.hostNo} {
		for name, c := range map[string]*http.Cookie{"viewa": x.va, "vall": x.vall} {
			if rec := get(c, h); rec.Code != http.StatusNotFound || rec.Body.String() != missing.Body.String() {
				t.Errorf("%s sin permiso, host %s: got %d %q, want 404 identico al inexistente", name, h, rec.Code, rec.Body.String())
			}
		}
	}

	// Con can_inventory: solo hosts de su ambito; el resto, 404 identico.
	x.patchUser(x.vaID, `{"can_inventory":true}`, http.StatusOK, "")
	if rec := get(x.va, x.hostA); rec.Code != http.StatusOK {
		t.Errorf("con permiso, host propio got %d, want 200", rec.Code)
	}
	for _, h := range []string{x.hostB, x.hostNo} {
		if rec := get(x.va, h); rec.Code != http.StatusNotFound || rec.Body.String() != missing.Body.String() {
			t.Errorf("con permiso, host ajeno %s: got %d %q, want 404 identico", h, rec.Code, rec.Body.String())
		}
	}
	if rec := get(x.va, "no-es-uuid"); rec.Code != http.StatusNotFound {
		t.Errorf("id malformado got %d, want 404", rec.Code)
	}

	// Admin: todos los hosts; inexistente 404.
	for _, h := range []string{x.hostA, x.hostB, x.hostNo} {
		if rec := get(x.admin, h); rec.Code != http.StatusOK {
			t.Errorf("admin host %s got %d, want 200", h, rec.Code)
		}
	}
	if rec := get(x.admin, missingID); rec.Code != http.StatusNotFound {
		t.Errorf("admin host inexistente got %d, want 404", rec.Code)
	}

	// can_inventory no se expone en hosts, resumen ni eventos de host.
	for _, p := range []string{"/api/v1/hosts", "/api/v1/hosts/" + x.hostA, "/api/v1/summary", "/api/v1/hosts/" + x.hostA + "/events"} {
		for _, c := range []*http.Cookie{x.va, x.admin} {
			if body := x.do("GET", p, "", withCookie(c)).Body.String(); strings.Contains(body, "inventory") {
				t.Errorf("GET %s expone inventario: %.200s", p, body)
			}
		}
	}
}

// enumRoute clasifica una ruta GET: public (sin datos de hosts), own (datos propios de la sesion), admin (403 al
// viewer restringido), inventory (404 identico sin can_inventory o con host ajeno) o scope (filtra por ambito; paths son las URL a probar, con ids ajenos).
type enumRoute struct {
	class string
	paths func(x *alertScopeEnv, checkB string) []string
}

func TestEnumerationGETRoutesRestrictedViewerLeaksNothing(t *testing.T) {
	x := newAlertScopeEnv(t)
	checkB := x.newCheck(fmt.Sprintf(`{"name":"tcp-b","type":"tcp","target_host":"localhost","target_port":23,"scope":{"type":"group","group_id":%q}}`, x.groupB))
	x.newCheck(`{"name":"tcp-all","type":"tcp","target_host":"localhost","target_port":22}`)
	devB := x.newDevice(`{"name":"dev-b","address":"10.1.1.1","collector_id":"` + x.hostB + `","group_id":"` + x.groupB + `"}`)
	registered := append([]route(nil), x.api.routes...) // copia: otros tests pueden registrar rutas

	fixed := func(p ...string) func(*alertScopeEnv, string) []string {
		return func(*alertScopeEnv, string) []string { return p }
	}
	const api = "/api/v1"
	invPaths := func(suffix string) enumRoute {
		return enumRoute{"inventory", func(x *alertScopeEnv, _ string) []string {
			return []string{api + "/hosts/" + x.hostB + suffix, api + "/hosts/" + x.hostNo + suffix, api + "/hosts/" + x.hostA + suffix}
		}}
	}
	updPaths := func(suffix string) enumRoute {
		return enumRoute{"updates", func(x *alertScopeEnv, _ string) []string {
			return []string{api + "/hosts/" + x.hostB + suffix, api + "/hosts/" + x.hostNo + suffix, api + "/hosts/" + x.hostA + suffix}
		}}
	}
	// Toda ruta GET nueva DEBE clasificarse aqui; si no, el test falla.
	table := map[string]enumRoute{
		"GET /health":                            {"public", fixed("/health")},
		"GET /install.sh":                        {"public", fixed("/install.sh")},
		"GET /install.ps1":                       {"public", fixed("/install.ps1")},
		"GET /dl/latest/manifest.json":           {"public", fixed("/dl/latest/manifest.json")},
		"GET /dl/{version}/{file}":               {"public", fixed("/dl/v0/argos-agent-linux-amd64")},
		"GET /":                                  {"public", fixed("/", "/hosts")},
		"GET /api/v1/auth/me":                    {"own", fixed(api + "/auth/me")},
		"GET /api/v1/users":                      {"admin", fixed(api + "/users")},
		"GET /api/v1/hosts/enroll-tokens":        {"admin", fixed(api + "/hosts/enroll-tokens")},
		"GET /api/v1/audit":                      {"admin", fixed(api + "/audit")},
		"GET /api/v1/audit/events":               {"admin", fixed(api + "/audit/events")},
		"GET /api/v1/channels":                   {"admin", fixed(api + "/channels")},
		"GET /api/v1/channels/{id}":              {"admin", fixed(api + "/channels/" + missingID)},
		"GET /api/v1/hosts":                      {"scope", fixed(api+"/hosts", api+"/hosts?q=host-b", api+"/hosts?group=none")},
		"GET /api/v1/snmp-profiles":              {"admin", fixed(api + "/snmp-profiles")},
		"GET /api/v1/settings/retention":         {"admin", fixed(api + "/settings/retention")},
		"GET /api/v1/system/status":              {"admin", fixed(api + "/system/status")},
		"GET /api/v1/snmp-profiles/{id}":         {"admin", fixed(api + "/snmp-profiles/" + missingID)},
		"GET /api/v1/devices":                    {"scope", fixed(api + "/devices")},
		"GET /api/v1/devices/{id}":               {"scope", fixed(api+"/devices/"+devB, api+"/devices/"+x.hostB)},
		"GET /api/v1/summary":                    {"scope", fixed(api + "/summary")},
		"GET /api/v1/reports/availability":       {"scope", fixed(api+"/reports/availability", api+"/reports/availability?by=group&range=7d")},
		"GET /api/v1/reports/service-checks":     {"scope", fixed(api + "/reports/service-checks")},
		"GET /api/v1/reports/availability.csv":   {"scope", fixed(api + "/reports/availability.csv")},
		"GET /api/v1/reports/service-checks.csv": {"scope", fixed(api + "/reports/service-checks.csv")},
		"GET /api/v1/reports/alerts.csv":         {"scope", fixed(api + "/reports/alerts.csv")},
		"GET /api/v1/groups":                     {"scope", fixed(api + "/groups")},
		"GET /api/v1/alerts":                     {"scope", fixed(api + "/alerts")},
		"GET /api/v1/alerts/summary":             {"scope", fixed(api + "/alerts/summary")},
		"GET /api/v1/alert-rules":                {"scope", fixed(api + "/alert-rules")},
		"GET /api/v1/alert-rules/{id}":           {"scope", fixed(api + "/alert-rules/" + missingID)},
		"GET /api/v1/service-checks":             {"scope", fixed(api + "/service-checks")},
		"GET /api/v1/maintenance":                {"scope", fixed(api + "/maintenance")},
		"GET /api/v1/maintenance/{id}":           {"scope", fixed(api + "/maintenance/" + missingID)},
		"GET /api/v1/service-checks/{id}":        {"scope", func(_ *alertScopeEnv, cb string) []string { return []string{api + "/service-checks/" + cb} }},
		"GET /api/v1/alerts/{id}": {"scope", func(x *alertScopeEnv, _ string) []string {
			return []string{api + "/alerts/" + x.alerts[x.hostB], api + "/alerts/" + x.alerts[x.hostNo]}
		}},
		"GET /api/v1/hosts/{id}": {"scope", func(x *alertScopeEnv, _ string) []string {
			return []string{api + "/hosts/" + x.hostB, api + "/hosts/" + x.hostNo}
		}},
		"GET /api/v1/hosts/{id}/metrics": {"scope", func(x *alertScopeEnv, _ string) []string {
			return []string{api + "/hosts/" + x.hostB + "/metrics", api + "/hosts/" + x.hostNo + "/metrics"}
		}},
		"GET /api/v1/hosts/{id}/history": {"scope", func(x *alertScopeEnv, _ string) []string {
			q := "?from=" + time.Now().Add(-time.Hour).UTC().Format(time.RFC3339) + "&to=" + time.Now().UTC().Format(time.RFC3339)
			return []string{api + "/hosts/" + x.hostB + "/history" + q, api + "/hosts/" + x.hostNo + "/history" + q + "&format=csv&series=disks"}
		}},
		"GET /api/v1/hosts/{id}/events": {"scope", func(x *alertScopeEnv, _ string) []string {
			return []string{api + "/hosts/" + x.hostB + "/events", api + "/hosts/" + x.hostNo + "/events"}
		}},
		"GET /api/v1/hosts/{id}/inventory":           invPaths("/inventory"),
		"GET /api/v1/hosts/{id}/inventory/ports":     invPaths("/inventory/ports"),
		"GET /api/v1/hosts/{id}/inventory/services":  invPaths("/inventory/services"),
		"GET /api/v1/hosts/{id}/inventory/processes": invPaths("/inventory/processes"),
		"GET /api/v1/hosts/{id}/inventory/changes":   invPaths("/inventory/changes"),
		"GET /api/v1/hosts/{id}/updates":             updPaths("/updates"),
		"GET /api/v1/updates":                        {"updates_fleet", fixed(api + "/updates")},
		"GET /api/v1/hosts/{id}/service-checks": {"scope", func(x *alertScopeEnv, _ string) []string {
			return []string{api + "/hosts/" + x.hostB + "/service-checks", api + "/hosts/" + x.hostNo + "/service-checks"}
		}},
		"GET /api/v1/hosts/{id}/service-checks/{check_id}/events": {"scope", func(x *alertScopeEnv, cb string) []string {
			return []string{api + "/hosts/" + x.hostB + "/service-checks/" + cb + "/events", api + "/hosts/" + x.hostNo + "/service-checks/" + cb + "/events"}
		}},
	}

	// La tabla y el mux deben coincidir exactamente en las rutas GET.
	seen := map[string]bool{}
	for _, r := range registered {
		if !strings.HasPrefix(r.pattern, "GET ") {
			continue
		}
		seen[r.pattern] = true
		if _, ok := table[r.pattern]; !ok {
			t.Errorf("ruta GET sin clasificar en la tabla de enumeracion: %q (clasificala: public/own/admin/scope)", r.pattern)
		}
	}
	var stale []string
	for p := range table {
		if !seen[p] && p != "GET /" { // el SPA solo se registra con ARGOS_WEB_DIR
			stale = append(stale, p)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("la tabla clasifica rutas que ya no existen: %v", stale)
	}

	// Datos del otro grupo (B) y sin grupo que no pueden aparecer en ninguna respuesta a viewa.
	forbidden := []string{x.hostB, x.hostNo, "host-b", "host-none", x.groupB, x.alerts[x.hostB], x.alerts[x.hostNo], checkB, "tcp-b", devB, "dev-b", "10.1.1.1"}

	for pattern, er := range table {
		for _, path := range er.paths(x, checkB) {
			rec := x.do("GET", path, "", withCookie(x.va))
			switch er.class {
			case "admin":
				if rec.Code != http.StatusForbidden {
					t.Errorf("%s (%s): viewer restringido got %d, want 403", pattern, path, rec.Code)
				}
			case "scope":
				// Los ids de la ruta son ajenos: el detalle debe ser 404; los listados, 200.
				want := http.StatusOK
				if strings.Contains(pattern, "{") {
					want = http.StatusNotFound
				}
				if rec.Code != want {
					t.Errorf("%s (%s): got %d, want %d", pattern, path, rec.Code, want)
				}
			case "inventory", "updates", "updates_fleet":
				// Sin el permiso de lectura (can_inventory / can_updates) todo es el mismo 404, incluso el host propio.
				if rec.Code != http.StatusNotFound {
					t.Errorf("%s (%s): sin permiso got %d, want 404", pattern, path, rec.Code)
				}
			case "public", "own":
				if rec.Code != http.StatusOK && rec.Code != http.StatusNotFound {
					t.Errorf("%s (%s): got %d, want 200/404", pattern, path, rec.Code)
				}
			}
			body := rec.Body.String()
			for _, f := range forbidden {
				// El id/nombre ajeno viaja en la URL: un 404 no debe repetirlo en el cuerpo.
				if f != "" && strings.Contains(body, f) {
					t.Errorf("%s (%s): la respuesta contiene dato ajeno %q: %.200s", pattern, path, f, body)
				}
			}
		}
	}

	// Con el permiso de su clase el viewer restringido ve su host (200) y el resto sigue siendo el mismo 404. Cada
	// permiso abre SOLO su clase: el otro se apaga antes pa comprobar que no se sustituyen.
	for _, pc := range []struct{ class, on, off string }{
		{"inventory", "can_inventory", "can_updates"},
		{"updates", "can_updates", "can_inventory"},
	} {
		x.patchUser(x.vaID, `{"`+pc.off+`":false}`, http.StatusOK, "")
		x.patchUser(x.vaID, `{"`+pc.on+`":true}`, http.StatusOK, "")
		for pattern, er := range table {
			if er.class != pc.class {
				continue
			}
			suffix := strings.TrimPrefix(pattern, "GET /api/v1/hosts/{id}")
			missing := x.do("GET", api+"/hosts/"+missingID+suffix, "", withCookie(x.va))
			for i, path := range er.paths(x, checkB) { // hostB, hostNo, hostA
				rec := x.do("GET", path, "", withCookie(x.va))
				if i < 2 && (rec.Code != http.StatusNotFound || rec.Body.String() != missing.Body.String()) {
					t.Errorf("%s (%s): con permiso y host ajeno got %d %q, want 404 identico", pattern, path, rec.Code, rec.Body.String())
				}
				if i == 2 && rec.Code != http.StatusOK {
					t.Errorf("%s (%s): con permiso y host propio got %d, want 200", pattern, path, rec.Code)
				}
			}
		}
	}
	// La flota de parches con permiso responde 200 y solo lista el ambito del usuario.
	rec := x.do("GET", api+"/updates", "", withCookie(x.va))
	if rec.Code != http.StatusOK {
		t.Errorf("GET /updates con can_updates got %d, want 200", rec.Code)
	}
	for _, f := range forbidden {
		if f != "" && strings.Contains(rec.Body.String(), f) {
			t.Errorf("GET /updates contiene dato ajeno %q", f)
		}
	}
}
