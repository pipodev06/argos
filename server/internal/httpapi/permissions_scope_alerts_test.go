package httpapi

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// Permisos por grupo (tanda 2): alertas, eventos, chequeos, reglas, mantenimiento, canales y auditoria.

// alertScopeEnv reutiliza la siembra de la tanda 1 (hosts A, B y sin grupo; viewa restringido a A) y deja una
// alerta abierta en cada host con una regla `all`.
type alertScopeEnv struct {
	*scopeEnv
	ae      *alertEnv
	ruleAll string
	alerts  map[string]string // host id -> alert id
}

func newAlertScopeEnv(t *testing.T) *alertScopeEnv {
	t.Helper()
	return newAlertScopeEnvWith(t, Options{HostDownAfter: 2 * time.Minute})
}

// newAlertScopeEnvWith es newAlertScopeEnv con opciones propias (canales, catalogo de binarios...).
func newAlertScopeEnvWith(t *testing.T, opts Options) *alertScopeEnv {
	t.Helper()
	s := newScopeEnvWith(t, opts)
	ae := &alertEnv{env: s.env, admin: s.admin, viewer: s.va}
	x := &alertScopeEnv{scopeEnv: s, ae: ae, alerts: map[string]string{}}
	for _, id := range []string{s.hostA, s.hostB, s.hostNo} {
		mustExec(t, `INSERT INTO host_latest (host_id, ts, cpu_pct, mem_used_mb, mem_total_mb) VALUES ($1, now(), 95, 100, 200)
			ON CONFLICT (host_id) DO UPDATE SET ts = now(), cpu_pct = 95`, id)
	}
	x.ruleAll = ae.createRule(cpuRuleBody)
	ae.eval()
	p := x.alertsOf(s.admin, "")
	if p.Total != 3 {
		t.Fatalf("siembra: admin ve %d alertas, want 3", p.Total)
	}
	for _, it := range p.Items {
		x.alerts[it.Host.ID] = it.ID
	}
	return x
}

func (x *alertScopeEnv) alertsOf(c *http.Cookie, query string) alertsPage {
	x.t.Helper()
	rec := x.do("GET", "/api/v1/alerts"+query, "", withCookie(c))
	x.wantStatus(rec, http.StatusOK, "")
	var p alertsPage
	x.dataAs(rec, &p)
	return p
}

func TestViewerRestrictedSeesOnlyAlertsOfItsGroups(t *testing.T) {
	x := newAlertScopeEnv(t)

	p := x.alertsOf(x.va, "")
	if p.Total != 1 || len(p.Items) != 1 || p.Items[0].Host.ID != x.hostA {
		t.Fatalf("lista de A got %+v, want solo la alerta de host-a", p)
	}
	// Filtros que apuntan a hosts ajenos: vacio, no error distinguible.
	for _, q := range []string{"?host_id=" + x.hostB, "?host_id=" + x.hostNo, "?host_id=" + x.hostB + "," + x.hostNo} {
		if got := x.alertsOf(x.va, q); got.Total != 0 || len(got.Items) != 0 {
			t.Errorf("filtro %q got %+v, want vacio", q, got)
		}
	}
	if got := x.alertsOf(x.va, "?host_id="+x.hostA+","+x.hostB); got.Total != 1 {
		t.Errorf("filtro mixto got total %d, want 1", got.Total)
	}

	// Detalle: propio 200; ajeno, sin grupo e inexistente: 404 identico.
	x.wantStatus(x.do("GET", "/api/v1/alerts/"+x.alerts[x.hostA], "", withCookie(x.va)), http.StatusOK, "")
	missing := x.do("GET", "/api/v1/alerts/"+missingID, "", withCookie(x.va))
	x.wantStatus(missing, http.StatusNotFound, "not_found")
	for _, h := range []string{x.hostB, x.hostNo} {
		rec := x.do("GET", "/api/v1/alerts/"+x.alerts[h], "", withCookie(x.va))
		x.wantStatus(rec, http.StatusNotFound, "not_found")
		if rec.Body.String() != missing.Body.String() {
			t.Errorf("404 de alerta ajena got %q, want identico al de una inexistente %q", rec.Body.String(), missing.Body.String())
		}
	}

	// Contadores: solo las suyas.
	var sum struct {
		Active int `json:"active"`
	}
	rec := x.do("GET", "/api/v1/alerts/summary", "", withCookie(x.va))
	x.wantStatus(rec, http.StatusOK, "")
	x.dataAs(rec, &sum)
	if sum.Active != 1 {
		t.Errorf("summary de A got %d, want 1", sum.Active)
	}
}

// newCheck crea un chequeo como admin y devuelve su id.
func (x *alertScopeEnv) newCheck(body string) string {
	x.t.Helper()
	rec := x.do("POST", "/api/v1/service-checks", body, withCookie(x.admin))
	x.wantStatus(rec, http.StatusCreated, "")
	var d struct {
		Check struct {
			ID string `json:"id"`
		} `json:"check"`
	}
	x.dataAs(rec, &d)
	return d.Check.ID
}

func (x *alertScopeEnv) checkNames(c *http.Cookie) []string {
	x.t.Helper()
	rec := x.do("GET", "/api/v1/service-checks", "", withCookie(c))
	x.wantStatus(rec, http.StatusOK, "")
	var d struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	x.dataAs(rec, &d)
	names := make([]string, 0, len(d.Items))
	for _, it := range d.Items {
		names = append(names, it.Name)
	}
	return names
}

func TestViewerRestrictedDoesNotSeeHostEventsNorChecksOfOtherHosts(t *testing.T) {
	x := newAlertScopeEnv(t)
	checkAll := x.newCheck(`{"name":"tcp-all","type":"tcp","target_host":"localhost","target_port":22}`)
	checkB := x.newCheck(fmt.Sprintf(`{"name":"tcp-b","type":"tcp","target_host":"localhost","target_port":23,"scope":{"type":"group","group_id":%q}}`, x.groupB))

	// Eventos del host y estado/eventos de chequeos: propio 200; ajeno y sin grupo 404.
	x.wantStatus(x.do("GET", "/api/v1/hosts/"+x.hostA+"/events", "", withCookie(x.va)), http.StatusOK, "")
	x.wantStatus(x.do("GET", "/api/v1/hosts/"+x.hostA+"/service-checks", "", withCookie(x.va)), http.StatusOK, "")
	x.wantStatus(x.do("GET", "/api/v1/hosts/"+x.hostA+"/service-checks/"+checkAll+"/events", "", withCookie(x.va)), http.StatusOK, "")
	for _, h := range []string{x.hostB, x.hostNo} {
		for _, path := range []string{"/events", "/service-checks", "/service-checks/" + checkAll + "/events"} {
			x.wantStatus(x.do("GET", "/api/v1/hosts/"+h+path, "", withCookie(x.va)), http.StatusNotFound, "not_found")
			x.wantStatus(x.do("GET", "/api/v1/hosts/"+h+path, "", withCookie(x.vall)), http.StatusOK, "")
		}
	}

	// Definiciones: el restringido ve la de alcance `all`, no la del grupo B.
	if got := x.checkNames(x.va); len(got) != 1 || got[0] != "tcp-all" {
		t.Errorf("chequeos de viewa got %v, want [tcp-all]", got)
	}
	x.wantStatus(x.do("GET", "/api/v1/service-checks/"+checkAll, "", withCookie(x.va)), http.StatusOK, "")
	x.wantStatus(x.do("GET", "/api/v1/service-checks/"+checkB, "", withCookie(x.va)), http.StatusNotFound, "not_found")
	for name, c := range map[string]*http.Cookie{"admin": x.admin, "viewer all_groups": x.vall} {
		if got := x.checkNames(c); len(got) != 2 {
			t.Errorf("%s: chequeos got %v, want 2", name, got)
		}
		x.wantStatus(x.do("GET", "/api/v1/service-checks/"+checkB, "", withCookie(c)), http.StatusOK, "")
	}
}

func TestRulesAndMaintenanceHideOtherGroupsAndLeakNothingAboutTheirHosts(t *testing.T) {
	x := newAlertScopeEnv(t)
	ruleBody := func(name, groupID string) string {
		return fmt.Sprintf(`{"name":%q,"kind":"metric","metric":"cpu_pct","op":"gt","threshold":90,"duration_seconds":0,"resolve_seconds":0,"severity":"warn","scope":{"type":"group","group_id":%q}}`, name, groupID)
	}
	ruleA := x.ae.createRule(ruleBody("cpu A", x.groupA))
	ruleB := x.ae.createRule(ruleBody("cpu B", x.groupB))
	x.ae.eval()

	type ruleView struct {
		ID           string   `json:"id"`
		ActiveAlerts int      `json:"active_alerts"`
		ChannelIDs   []string `json:"channel_ids"`
	}
	rules := func(c *http.Cookie) map[string]ruleView {
		rec := x.do("GET", "/api/v1/alert-rules", "", withCookie(c))
		x.wantStatus(rec, http.StatusOK, "")
		var d struct {
			Items []ruleView `json:"items"`
		}
		x.dataAs(rec, &d)
		m := map[string]ruleView{}
		for _, r := range d.Items {
			m[r.ID] = r
		}
		return m
	}

	// Restringido: la regla `all` y la de su grupo; nunca la de B. La `all` sigue evaluando todos los hosts,
	// pero su contador solo cuenta las alertas de sus hosts.
	got := rules(x.va)
	if _, seesB := got[ruleB]; len(got) != 2 || seesB {
		t.Fatalf("reglas de viewa got %+v, want solo `all` y A", got)
	}
	if got[x.ruleAll].ActiveAlerts != 1 || got[ruleA].ActiveAlerts != 1 {
		t.Errorf("active_alerts got all=%d A=%d, want 1 y 1", got[x.ruleAll].ActiveAlerts, got[ruleA].ActiveAlerts)
	}
	// Admin y viewer all_groups: todo, con los conteos globales.
	for name, c := range map[string]*http.Cookie{"admin": x.admin, "viewer all_groups": x.vall} {
		all := rules(c)
		if len(all) != 3 || all[x.ruleAll].ActiveAlerts != 3 || all[ruleB].ActiveAlerts != 1 {
			t.Errorf("%s: reglas got %+v", name, all)
		}
	}
	x.wantStatus(x.do("GET", "/api/v1/alert-rules/"+ruleA, "", withCookie(x.va)), http.StatusOK, "")
	allRec := x.do("GET", "/api/v1/alert-rules/"+x.ruleAll, "", withCookie(x.va))
	x.wantStatus(allRec, http.StatusOK, "")
	var one struct {
		Rule ruleView `json:"rule"`
	}
	x.dataAs(allRec, &one)
	if one.Rule.ActiveAlerts != 1 {
		t.Errorf("detalle de la regla `all` para viewa got active_alerts %d, want 1", one.Rule.ActiveAlerts)
	}
	missing := x.do("GET", "/api/v1/alert-rules/"+missingID, "", withCookie(x.va))
	rec := x.do("GET", "/api/v1/alert-rules/"+ruleB, "", withCookie(x.va))
	x.wantStatus(rec, http.StatusNotFound, "not_found")
	if rec.Body.String() != missing.Body.String() {
		t.Errorf("404 de regla ajena got %q, want %q", rec.Body.String(), missing.Body.String())
	}

	// Mantenimiento: ventana `all` y de A visibles; la de B no.
	wAll := x.ae.createWindow(`{"name":"todos","scope":{"type":"all"}}`, time.Hour)
	wA := x.ae.createWindow(fmt.Sprintf(`{"name":"solo A","scope":{"type":"group","group_id":%q}}`, x.groupA), time.Hour)
	wB := x.ae.createWindow(fmt.Sprintf(`{"name":"solo B","scope":{"type":"group","group_id":%q}}`, x.groupB), time.Hour)
	windows := func(c *http.Cookie) map[string]bool {
		rec := x.do("GET", "/api/v1/maintenance", "", withCookie(c))
		x.wantStatus(rec, http.StatusOK, "")
		var d struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		}
		x.dataAs(rec, &d)
		m := map[string]bool{}
		for _, it := range d.Items {
			m[it.ID] = true
		}
		return m
	}
	if got := windows(x.va); len(got) != 2 || got[wB] {
		t.Errorf("ventanas de viewa got %v, want solo todos y A", got)
	}
	for _, c := range []*http.Cookie{x.admin, x.vall} {
		if got := windows(c); len(got) != 3 {
			t.Errorf("ventanas de admin/all_groups got %v, want 3", got)
		}
	}
	x.wantStatus(x.do("GET", "/api/v1/maintenance/"+wAll, "", withCookie(x.va)), http.StatusOK, "")
	x.wantStatus(x.do("GET", "/api/v1/maintenance/"+wA, "", withCookie(x.va)), http.StatusOK, "")
	x.wantStatus(x.do("GET", "/api/v1/maintenance/"+wB, "", withCookie(x.va)), http.StatusNotFound, "not_found")
}

func TestChannelsAndAuditAreDeniedToRestrictedViewers(t *testing.T) {
	x := newAlertScopeEnv(t)
	// Restringido: 403 (como un viewer en una ruta de admin), exista o no el canal (sin oraculo de existencia).
	for _, path := range []string{"/api/v1/channels", "/api/v1/channels/" + missingID} {
		x.wantStatus(x.do("GET", path, "", withCookie(x.va)), http.StatusForbidden, "")
	}
	// Admin y viewer all_groups: como hoy.
	for _, c := range []*http.Cookie{x.admin, x.vall} {
		x.wantStatus(x.do("GET", "/api/v1/channels", "", withCookie(c)), http.StatusOK, "")
		x.wantStatus(x.do("GET", "/api/v1/channels/"+missingID, "", withCookie(c)), http.StatusNotFound, "not_found")
	}
	// Auditoria: solo admin para todo viewer (ya lo era; se fija).
	for _, path := range []string{"/api/v1/audit", "/api/v1/audit/events"} {
		x.wantStatus(x.do("GET", path, "", withCookie(x.va)), http.StatusForbidden, "")
		x.wantStatus(x.do("GET", path, "", withCookie(x.vall)), http.StatusForbidden, "")
		x.wantStatus(x.do("GET", path, "", withCookie(x.admin)), http.StatusOK, "")
	}
}

func TestAlertScopeChangeAppliesOnNextRequestAndCursorsAreBoundToTheScope(t *testing.T) {
	x := newAlertScopeEnv(t)

	// El cursor del admin (ambito total) no sirve a un restringido.
	var page alertsPage
	rec := x.do("GET", "/api/v1/alerts?limit=1", "", withCookie(x.admin))
	x.wantStatus(rec, http.StatusOK, "")
	x.dataAs(rec, &page)
	if page.NextCursor == nil {
		t.Fatal("el admin debe tener siguiente pagina")
	}
	x.wantStatus(x.do("GET", "/api/v1/alerts?limit=1&cursor="+*page.NextCursor, "", withCookie(x.va)), http.StatusBadRequest, "invalid_cursor")

	// Con A y B hay dos paginas; al cambiar el ambito el cursor previo se rechaza.
	x.patchUser(x.vaID, `{"group_ids":["`+x.groupA+`","`+x.groupB+`"]}`, http.StatusOK, "")
	var two alertsPage
	rec = x.do("GET", "/api/v1/alerts?limit=1", "", withCookie(x.va))
	x.wantStatus(rec, http.StatusOK, "")
	x.dataAs(rec, &two)
	if two.Total != 2 || two.NextCursor == nil {
		t.Fatalf("con A y B got %+v, want total 2 y cursor", two)
	}
	x.patchUser(x.vaID, `{"group_ids":["`+x.groupA+`"]}`, http.StatusOK, "")
	x.wantStatus(x.do("GET", "/api/v1/alerts?limit=1&cursor="+*two.NextCursor, "", withCookie(x.va)), http.StatusBadRequest, "invalid_cursor")

	// Revocar el grupo: en la siguiente peticion ya no ve nada.
	x.patchUser(x.vaID, `{"group_ids":[]}`, http.StatusOK, "")
	if p := x.alertsOf(x.va, ""); p.Total != 0 || len(p.Items) != 0 {
		t.Errorf("tras revocar got %+v, want vacio", p)
	}
	x.wantStatus(x.do("GET", "/api/v1/alerts/"+x.alerts[x.hostA], "", withCookie(x.va)), http.StatusNotFound, "not_found")
	x.wantStatus(x.do("GET", "/api/v1/hosts/"+x.hostA+"/events", "", withCookie(x.va)), http.StatusNotFound, "not_found")
	x.wantStatus(x.do("GET", "/api/v1/hosts/"+x.hostA+"/service-checks", "", withCookie(x.va)), http.StatusNotFound, "not_found")
}

func TestViewerAllGroupsSeesAllAlertsUnchanged(t *testing.T) {
	x := newAlertScopeEnv(t)
	for name, c := range map[string]*http.Cookie{"viewer all_groups": x.vall, "admin": x.admin} {
		if p := x.alertsOf(c, ""); p.Total != 3 || len(p.Items) != 3 {
			t.Errorf("%s: alertas got %+v, want 3", name, p)
		}
		for _, id := range x.alerts {
			x.wantStatus(x.do("GET", "/api/v1/alerts/"+id, "", withCookie(c)), http.StatusOK, "")
		}
		if p := x.alertsOf(c, "?host_id="+x.hostNo); p.Total != 1 {
			t.Errorf("%s: filtro host sin grupo got %+v, want 1", name, p)
		}
	}
}
