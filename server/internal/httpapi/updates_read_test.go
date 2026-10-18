package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"argos/server/internal/store"
)

// Lectura de parches del SO contra Postgres real: permiso x ambito, contrato, auditoria, flota y alertas.

func bp(v bool) *bool { return &v }
func ip(v int) *int   { return &v }

type updReadEnv struct {
	*scopeEnv
	rev int
}

// put guarda una foto de parches del host (nil = desconocido).
func (x *updReadEnv) put(hostID string, total, sec *int, reboot *bool) {
	x.t.Helper()
	x.rev++
	in := store.UpdatesSnapshot{Rev: fmt.Sprintf("%064x", x.rev), PendingTotal: total, PendingSecurity: sec, RebootRequired: reboot,
		Source: "apt", CacheAgeS: func() *int64 { v := int64(3600); return &v }(), Advisories: []string{"USN-1"}, CollectedAt: time.Now()}
	if err := x.st.IngestUpdates(context.Background(), hostID, in, time.Now()); err != nil {
		x.t.Fatal(err)
	}
}

// newUpdReadEnv: hostA (grupo A) y hostB (grupo B) con datos; hostNo (sin grupo) sin datos. va (grupo A) y admin;
// va NO tiene can_updates todavia.
func newUpdReadEnv(t *testing.T) *updReadEnv {
	t.Helper()
	x := &updReadEnv{scopeEnv: newScopeEnv(t)}
	x.put(x.hostA, ip(12), ip(5), bp(true))
	x.put(x.hostB, ip(3), nil, nil)
	return x
}

func (x *updReadEnv) get(c *http.Cookie, path string) (int, string) {
	rec := x.do("GET", "/api/v1"+path, "", withCookie(c))
	return rec.Code, rec.Body.String()
}

type hostUpdatesResp struct {
	Status  string `json:"status"`
	Updates *struct {
		PendingTotal    *int    `json:"pending_total"`
		PendingSecurity *int    `json:"pending_security"`
		RebootRequired  *bool   `json:"reboot_required"`
		LastInstalled   *string `json:"last_installed"`
		Known           struct {
			PendingTotal    bool `json:"pending_total"`
			PendingSecurity bool `json:"pending_security"`
			RebootRequired  bool `json:"reboot_required"`
			LastInstalled   bool `json:"last_installed"`
		} `json:"known"`
		Source     string   `json:"source"`
		Stale      bool     `json:"stale"`
		DataAgeS   int64    `json:"data_age_s"`
		Advisories []string `json:"advisories"`
	} `json:"updates"`
	EOL *struct {
		Label     string `json:"label"`
		EOLDate   string `json:"eol_date"`
		DaysLeft  int    `json:"days_left"`
		Expired   bool   `json:"expired"`
		Estimated bool   `json:"estimated"`
	} `json:"eol"`
	History []struct {
		PendingTotal *int `json:"pending_total"`
	} `json:"history"`
}

type fleetResp struct {
	Items []struct {
		Host struct {
			ID       string `json:"id"`
			Hostname string `json:"hostname"`
		} `json:"host"`
		PendingSecurity *int  `json:"pending_security"`
		RebootRequired  *bool `json:"reboot_required"`
		EOL             *struct {
			Expired bool `json:"expired"`
		} `json:"eol"`
	} `json:"items"`
	NextCursor *string `json:"next_cursor"`
	Total      int     `json:"total"`
}

func TestUpdatesReadPermissionsAndScope(t *testing.T) {
	x := newUpdReadEnv(t)
	_, missingBody := x.get(x.admin, "/hosts/"+missingID+"/updates")
	if code, _ := x.get(x.admin, "/hosts/"+missingID+"/updates"); code != http.StatusNotFound {
		t.Fatalf("host inexistente got %d", code)
	}
	// Sin can_updates (ni siquiera con can_inventory): mismo 404 en el host propio, el ajeno y la flota.
	x.patchUser(x.vaID, `{"can_inventory":true}`, http.StatusOK, "")
	for _, p := range []string{"/hosts/" + x.hostA + "/updates", "/hosts/" + x.hostB + "/updates", "/updates"} {
		for name, c := range map[string]*http.Cookie{"va": x.va, "vall": x.vall} {
			if code, body := x.get(c, p); code != http.StatusNotFound || body != missingBody {
				t.Errorf("%s sin can_updates en %s got %d %q, want 404 identico", name, p, code, body)
			}
		}
	}
	// Con can_updates: su host 200, el ajeno el mismo 404, y la flota solo su ambito.
	x.patchUser(x.vaID, `{"can_updates":true,"can_inventory":false}`, http.StatusOK, "")
	if code, _ := x.get(x.va, "/hosts/"+x.hostA+"/updates"); code != http.StatusOK {
		t.Errorf("host propio got %d, want 200", code)
	}
	if code, body := x.get(x.va, "/hosts/"+x.hostB+"/updates"); code != http.StatusNotFound || body != missingBody {
		t.Errorf("host ajeno got %d %q, want 404 identico", code, body)
	}
	if code, body := x.get(x.va, "/hosts/"+x.hostNo+"/updates"); code != http.StatusNotFound || body != missingBody {
		t.Errorf("host sin grupo got %d %q, want 404 identico", code, body)
	}
	var fl fleetResp
	x.dataAs(x.do("GET", "/api/v1/updates", "", withCookie(x.va)), &fl)
	if fl.Total != 1 || len(fl.Items) != 1 || fl.Items[0].Host.ID != x.hostA {
		t.Errorf("flota de va got %+v, want solo hostA", fl)
	}
	x.dataAs(x.do("GET", "/api/v1/updates", "", withCookie(x.admin)), &fl)
	if fl.Total != 2 {
		t.Errorf("flota de admin total got %d, want 2 (hostNo no reporto)", fl.Total)
	}
	// Un grupo ajeno en el filtro no filtra nada fuera del ambito.
	x.dataAs(x.do("GET", "/api/v1/updates?group="+x.groupB, "", withCookie(x.va)), &fl)
	if fl.Total != 0 || len(fl.Items) != 0 {
		t.Errorf("va pidiendo el grupo B got %+v, want vacio", fl)
	}
}

func TestUpdatesReadContract(t *testing.T) {
	x := newUpdReadEnv(t)
	// hostA con SO Debian 11 (fin de soporte 2026-08-31, oficial): ya vencido.
	mustExec(t, `UPDATE hosts SET os = 'linux' WHERE id = $1`, x.hostA)
	mustExec(t, `INSERT INTO host_info (host_id, os_name, os_version) VALUES ($1, 'debian', '11.9')`, x.hostA)
	x.put(x.hostA, ip(12), ip(5), bp(true))

	rec := x.do("GET", "/api/v1/hosts/"+x.hostA+"/updates", "", withCookie(x.admin))
	x.wantStatus(rec, http.StatusOK, "")
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control got %q, want no-store", cc)
	}
	var got hostUpdatesResp
	x.dataAs(rec, &got)
	u := got.Updates
	if got.Status != "ok" || u == nil || *u.PendingTotal != 12 || *u.PendingSecurity != 5 || !*u.RebootRequired || u.Source != "apt" ||
		!u.Known.PendingTotal || !u.Known.PendingSecurity || !u.Known.RebootRequired || u.Known.LastInstalled || u.LastInstalled != nil ||
		u.Stale || u.DataAgeS < 0 || len(u.Advisories) != 1 {
		t.Errorf("foto got %+v (status %s)", u, got.Status)
	}
	if got.EOL == nil || got.EOL.Label != "Debian 11" || got.EOL.EOLDate != "2026-08-31" || !got.EOL.Expired || got.EOL.DaysLeft >= 0 || got.EOL.Estimated {
		t.Errorf("eol got %+v, want Debian 11 vencido, oficial", got.EOL)
	}
	if len(got.History) != 1 { // los dos put no cambian los contadores: solo la linea base
		t.Errorf("historial got %d puntos, want 1", len(got.History))
	}

	// Desconocido = null y known=false (nunca 0/false); sin EOL para su SO.
	rec = x.do("GET", "/api/v1/hosts/"+x.hostB+"/updates", "", withCookie(x.admin))
	got = hostUpdatesResp{}
	x.dataAs(rec, &got)
	if got.Updates == nil || got.Updates.PendingSecurity != nil || got.Updates.RebootRequired != nil || got.Updates.Known.PendingSecurity ||
		got.Updates.Known.RebootRequired || !got.Updates.Known.PendingTotal || got.EOL != nil {
		t.Errorf("host con datos desconocidos got %+v", got)
	}
	if !strings.Contains(rec.Body.String(), `"pending_security":null`) || !strings.Contains(rec.Body.String(), `"eol":null`) {
		t.Errorf("el desconocido debe salir como null: %s", rec.Body.String())
	}
	// Host visible que nunca reporto: no_data con historial vacio (no 404).
	rec = x.do("GET", "/api/v1/hosts/"+x.hostNo+"/updates", "", withCookie(x.admin))
	got = hostUpdatesResp{}
	x.dataAs(rec, &got)
	if got.Status != "no_data" || got.Updates != nil || got.History == nil || len(got.History) != 0 {
		t.Errorf("sin datos got %+v", got)
	}
	// ?history=N acotado.
	for q, want := range map[string]int{"0": http.StatusOK, "50": http.StatusOK, "51": http.StatusBadRequest, "-1": http.StatusBadRequest, "x": http.StatusBadRequest} {
		if code, _ := x.get(x.admin, "/hosts/"+x.hostA+"/updates?history="+q); code != want {
			t.Errorf("history=%s got %d, want %d", q, code, want)
		}
	}
	if code, _ := x.get(x.admin, "/hosts/no-es-uuid/updates"); code != http.StatusNotFound {
		t.Errorf("id invalido got %d, want 404", code)
	}
}

func TestUpdatesFleetFiltersOrderAndCursor(t *testing.T) {
	x := newUpdReadEnv(t)
	mustExec(t, `UPDATE hosts SET os = 'linux' WHERE id = $1`, x.hostA)
	mustExec(t, `INSERT INTO host_info (host_id, os_name, os_version) VALUES ($1, 'debian', '11.9')`, x.hostA)
	fleet := func(q string) fleetResp {
		t.Helper()
		var fl fleetResp
		rec := x.do("GET", "/api/v1/updates"+q, "", withCookie(x.admin))
		x.wantStatus(rec, http.StatusOK, "")
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("Cache-Control got %q, want no-store", cc)
		}
		x.dataAs(rec, &fl)
		return fl
	}
	if fl := fleet(""); len(fl.Items) != 2 || fl.Items[0].Host.ID != x.hostA || fl.Items[1].Host.ID != x.hostB {
		t.Errorf("orden por defecto (seguridad, desconocido al final) got %+v", fl)
	}
	if fl := fleet("?security=true"); fl.Total != 1 || fl.Items[0].Host.ID != x.hostA {
		t.Errorf("security=true got %+v", fl)
	}
	if fl := fleet("?reboot=1"); fl.Total != 1 || fl.Items[0].Host.ID != x.hostA {
		t.Errorf("reboot=1 got %+v", fl)
	}
	if fl := fleet("?eol_days=0"); fl.Total != 1 || fl.Items[0].Host.ID != x.hostA || fl.Items[0].EOL == nil || !fl.Items[0].EOL.Expired {
		t.Errorf("eol_days=0 got %+v", fl)
	}
	// Cursor: dos paginas de 1, y el cursor solo vale pa la misma consulta.
	p1 := fleet("?limit=1")
	if len(p1.Items) != 1 || p1.NextCursor == nil || p1.Total != 2 {
		t.Fatalf("pagina 1 got %+v", p1)
	}
	p2 := fleet("?limit=1&cursor=" + *p1.NextCursor)
	if len(p2.Items) != 1 || p2.NextCursor != nil || p2.Items[0].Host.ID == p1.Items[0].Host.ID {
		t.Errorf("pagina 2 got %+v", p2)
	}
	for name, q := range map[string]string{
		"cursor de otra consulta": "?limit=1&sort=name&cursor=" + *p1.NextCursor,
		"cursor roto":             "?cursor=abc.def",
		"cursor largo":            "?cursor=" + strings.Repeat("a", maxCursorLen+1),
		"orden malo":              "?sort=hack",
		"dir mala":                "?dir=up",
		"limite cero":             "?limit=0",
		"limite enorme":           "?limit=101",
		"security malo":           "?security=quiza",
		"eol_days negativo":       "?eol_days=-1",
		"eol_days enorme":         "?eol_days=3651",
		"grupo malo":              "?group=zzz",
		"os malo":                 "?os=beos",
		"q largo":                 "?q=" + strings.Repeat("a", maxQueryLen+1),
	} {
		if code, _ := x.get(x.admin, "/updates"+q); code != http.StatusBadRequest {
			t.Errorf("%s got %d, want 400", name, code)
		}
	}
	for _, s := range []string{"security", "pending", "reboot", "eol", "name", "updated"} {
		if code, _ := x.get(x.admin, "/updates?sort="+s+"&dir=asc"); code != http.StatusOK {
			t.Errorf("sort=%s got %d, want 200", s, code)
		}
	}
}

func TestUpdatesReadAuditDedup(t *testing.T) {
	x := newUpdReadEnv(t)
	x.patchUser(x.vaID, `{"can_updates":true}`, http.StatusOK, "")
	for range 3 { // refrescos del frontend: un solo evento por (usuario, destino)
		x.get(x.va, "/hosts/"+x.hostA+"/updates")
		x.get(x.va, "/updates")
	}
	x.get(x.admin, "/hosts/"+x.hostA+"/updates")
	q := `SELECT count(*) FROM audit_log WHERE event = 'updates_read' AND actor_username = $1`
	if n := x.count(q+` AND target_id = $2`, "viewa", x.hostA); n != 1 {
		t.Errorf("eventos de viewa sobre hostA got %d, want 1", n)
	}
	if n := x.count(q, "viewa"); n != 2 { // host y flota
		t.Errorf("eventos de viewa got %d, want 2 (host y flota)", n)
	}
	if n := x.count(q, "admin"); n != 1 {
		t.Errorf("eventos de admin got %d, want 1", n)
	}
	if n := x.count(`SELECT count(*) FROM audit_log WHERE event = 'updates_read' AND detail::text NOT IN ('{"scope": "host", "module": "updates"}', '{"scope": "fleet", "module": "updates"}')`); n != 0 {
		t.Errorf("el detalle debe llevar solo module y scope, sin contenido (%d filas distintas)", n)
	}
	// La ventana (1 h) supera la inactividad de sesion (30 min): se avanza en tramos con una peticion
	// que no audita (/auth/me) pa mantener viva la sesion.
	for range 3 {
		x.clock.Advance(invAuditWindow/3 + time.Second)
		for _, c := range []*http.Cookie{x.admin, x.va, x.vall} {
			x.do("GET", "/api/v1/auth/me", "", withCookie(c))
		}
	}
	x.get(x.admin, "/hosts/"+x.hostA+"/updates")
	if n := x.count(q, "admin"); n != 2 {
		t.Errorf("tras la ventana got %d, want 2", n)
	}
	// Un 404 (host ajeno) no se audita como lectura; la falta de permiso deja authz_denied con tope.
	before := x.count(`SELECT count(*) FROM audit_log WHERE event = 'updates_read'`)
	x.get(x.va, "/hosts/"+x.hostB+"/updates")
	if n := x.count(`SELECT count(*) FROM audit_log WHERE event = 'updates_read'`); n != before {
		t.Errorf("un 404 audito una lectura (%d -> %d)", before, n)
	}
	for range 3 {
		x.get(x.vall, "/updates")
	}
	if n := x.count(`SELECT count(*) FROM audit_log WHERE event = 'authz_denied' AND actor_username = 'vall' AND detail->>'route' = 'GET /api/v1/updates'`); n != 1 {
		t.Errorf("authz_denied por falta de can_updates got %d, want 1 (con tope de tasa)", n)
	}
}

func TestUpdatesNotInHostViews(t *testing.T) {
	x := newUpdReadEnv(t)
	for _, p := range []string{"/hosts", "/hosts/" + x.hostA, "/summary", "/hosts/" + x.hostA + "/events", "/alerts"} {
		for _, c := range []*http.Cookie{x.va, x.admin} {
			body := x.do("GET", "/api/v1"+p, "", withCookie(c)).Body.String()
			for _, w := range []string{"pending_security", "reboot_required", "advisories"} {
				if strings.Contains(body, w) {
					t.Errorf("GET %s expone parches (%s): %.200s", p, w, body)
				}
			}
		}
	}
}

func TestUpdatesRuleValidationAPI(t *testing.T) {
	e := newAlertEnv(t)
	post := func(body string) int { return e.do("POST", "/api/v1/alert-rules", body, withCookie(e.admin)).Code }
	bad := map[string]string{
		"pending sin umbral":       `{"name":"a1","kind":"pending_updates","duration_seconds":0,"severity":"warn"}`,
		"pending umbral 0":         `{"name":"a2","kind":"pending_updates","threshold":0,"duration_seconds":0,"severity":"warn"}`,
		"pending umbral decimal":   `{"name":"a3","kind":"pending_updates","threshold":1.5,"duration_seconds":0,"severity":"warn"}`,
		"pending umbral enorme":    `{"name":"a4","kind":"pending_updates","threshold":10001,"duration_seconds":0,"severity":"warn"}`,
		"pending con metric":       `{"name":"a5","kind":"pending_updates","threshold":1,"metric":"cpu_pct","duration_seconds":0,"severity":"warn"}`,
		"pending con clear":        `{"name":"a6","kind":"pending_updates","threshold":1,"clear_threshold":1,"duration_seconds":0,"severity":"warn"}`,
		"pending dias negativos":   `{"name":"a7","kind":"pending_updates","threshold":1,"duration_days":-1,"duration_seconds":0,"severity":"warn"}`,
		"pending dias enormes":     `{"name":"a8","kind":"pending_updates","threshold":1,"duration_days":366,"duration_seconds":0,"severity":"warn"}`,
		"reboot dias 0":            `{"name":"b1","kind":"reboot_pending","threshold":0,"duration_seconds":0,"severity":"warn"}`,
		"reboot dias 366":          `{"name":"b2","kind":"reboot_pending","threshold":366,"duration_seconds":0,"severity":"warn"}`,
		"reboot con duration_days": `{"name":"b3","kind":"reboot_pending","threshold":3,"duration_days":1,"duration_seconds":0,"severity":"warn"}`,
		"eol negativo":             `{"name":"c1","kind":"os_eol","threshold":-1,"duration_seconds":0,"severity":"warn"}`,
		"eol 731":                  `{"name":"c2","kind":"os_eol","threshold":731,"duration_seconds":0,"severity":"warn"}`,
		"eol sin umbral":           `{"name":"c3","kind":"os_eol","duration_seconds":0,"severity":"warn"}`,
		"metric con duration_days": `{"name":"d1","kind":"metric","metric":"cpu_pct","op":"gt","threshold":90,"duration_days":1,"duration_seconds":0,"severity":"warn"}`,
		"expected_ports en parche": `{"name":"e1","kind":"reboot_pending","threshold":3,"duration_seconds":0,"severity":"warn","expected_ports":[{"proto":"tcp","port":1}]}`,
	}
	for name, body := range bad {
		if code := post(body); code != http.StatusBadRequest {
			t.Errorf("%s: status got %d, want 400", name, code)
		}
	}
	id := e.createRule(`{"name":"sec","kind":"pending_updates","threshold":3,"duration_days":7,"duration_seconds":0,"severity":"warn"}`)
	e.createRule(`{"name":"reinicio","kind":"reboot_pending","threshold":3,"duration_seconds":0,"severity":"info"}`)
	e.createRule(`{"name":"eol","kind":"os_eol","threshold":0,"duration_seconds":0,"severity":"warn"}`)
	rec := e.do("PATCH", "/api/v1/alert-rules/"+id, `{"duration_days":14,"threshold":5}`, withCookie(e.admin))
	e.wantStatus(rec, http.StatusOK, "")
	var got struct {
		Rule struct {
			Kind         string  `json:"kind"`
			Threshold    float64 `json:"threshold"`
			DurationDays int     `json:"duration_days"`
		} `json:"rule"`
	}
	e.dataAs(rec, &got)
	if got.Rule.Kind != "pending_updates" || got.Rule.Threshold != 5 || got.Rule.DurationDays != 14 {
		t.Errorf("regla got %+v", got.Rule)
	}
	e.wantStatus(e.do("PATCH", "/api/v1/alert-rules/"+id, `{"threshold":2.5}`, withCookie(e.admin)), http.StatusBadRequest, "")
	e.wantStatus(e.do("PATCH", "/api/v1/alert-rules/"+id, `{"duration_days":400}`, withCookie(e.admin)), http.StatusBadRequest, "")
	// El resto de los kinds informa duration_days = 0.
	mid := e.createRule(cpuRuleBody)
	if body := e.do("GET", "/api/v1/alert-rules/"+mid, "", withCookie(e.viewer)).Body.String(); !strings.Contains(body, `"duration_days":0`) {
		t.Errorf("una regla metric debe llevar duration_days 0: %s", body)
	}
}

func TestUpdatesAlertsDetailNeedsUpdatesPermission(t *testing.T) {
	e := newAlertEnv(t)
	e.newUser("upduser", pwViewer, store.RoleViewer, false)
	e.newUser("invuser", pwViewer, store.RoleViewer, false)
	mustExec(t, `UPDATE users SET can_updates = true WHERE username = 'upduser'`)
	mustExec(t, `UPDATE users SET can_inventory = true WHERE username = 'invuser'`)
	updCookie, invCookie := e.login("upduser", pwViewer), e.login("invuser", pwViewer)

	hostID, _, _ := e.enrollHost(e.admin, "srv")
	e.createRule(`{"name":"sec","kind":"pending_updates","threshold":1,"duration_seconds":0,"resolve_seconds":0,"severity":"warn"}`)
	e.createRule(`{"name":"reinicio","kind":"reboot_pending","threshold":1,"duration_seconds":0,"resolve_seconds":0,"severity":"warn"}`)
	e.createRule(`{"name":"eol","kind":"os_eol","threshold":30,"duration_seconds":0,"resolve_seconds":0,"severity":"warn"}`)
	mustExec(t, `UPDATE hosts SET os = 'linux' WHERE id = $1`, hostID)
	mustExec(t, `INSERT INTO host_info (host_id, os_name, os_version) VALUES ($1, 'debian', '11.9') ON CONFLICT (host_id) DO UPDATE SET os_name = 'debian', os_version = '11.9'`, hostID)
	in := store.UpdatesSnapshot{Rev: strings.Repeat("d", 64), PendingTotal: ip(12), PendingSecurity: ip(5), RebootRequired: bp(true),
		Source: "apt", Advisories: []string{}, CollectedAt: time.Now()}
	if err := e.st.IngestUpdates(context.Background(), hostID, in, time.Now()); err != nil {
		t.Fatal(err)
	}
	mustExec(t, `UPDATE updates_current SET reboot_since = now() - interval '3 days'`)
	if r := e.eval(); r.Opened != 3 {
		t.Fatalf("opened got %d, want 3", r.Opened)
	}

	detailsFor := func(c *http.Cookie) map[string]*string {
		rec := e.do("GET", "/api/v1/alerts", "", withCookie(c))
		e.wantStatus(rec, http.StatusOK, "")
		var p struct {
			Items []struct {
				Kind         string   `json:"kind"`
				Severity     string   `json:"severity"`
				Detail       *string  `json:"detail"`
				TriggerValue *float64 `json:"trigger_value"`
			} `json:"items"`
		}
		e.dataAs(rec, &p)
		out := map[string]*string{}
		for _, it := range p.Items {
			if it.TriggerValue == nil {
				t.Errorf("%s: el conteo (trigger_value) debe quedar aunque no haya detalle", it.Kind)
			}
			if it.Kind == "os_eol" && it.Severity != "critical" {
				t.Errorf("os_eol vencido debe abrir critica, got %s", it.Severity)
			}
			out[it.Kind] = it.Detail
		}
		if len(out) != 3 {
			t.Fatalf("alertas got %v, want los 3 kinds", out)
		}
		return out
	}
	for name, c := range map[string]*http.Cookie{"viewer sin permisos": e.viewer, "con can_inventory": invCookie} {
		for kind, d := range detailsFor(c) {
			if d != nil {
				t.Errorf("%s: detail de %s got %q, want null", name, kind, *d)
			}
		}
	}
	for name, c := range map[string]*http.Cookie{"admin": e.admin, "con can_updates": updCookie} {
		d := detailsFor(c)
		if d["pending_updates"] == nil || *d["pending_updates"] != "5 de seguridad, 12 en total" {
			t.Errorf("%s: pending_updates detail got %v", name, d["pending_updates"])
		}
		if d["reboot_pending"] == nil || !strings.HasPrefix(*d["reboot_pending"], "reinicio pendiente desde 20") {
			t.Errorf("%s: reboot_pending detail got %v", name, d["reboot_pending"])
		}
		if d["os_eol"] == nil || *d["os_eol"] != "Debian 11, fin de soporte 2026-08-31" {
			t.Errorf("%s: os_eol detail got %v", name, d["os_eol"])
		}
	}
}
