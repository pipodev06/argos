package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"argos/server/internal/store"
)

// API de alertas, reglas, mantenimiento y eventos de host contra Postgres real (store real, reloj controlable).

const missingID = "00000000-0000-4000-8000-0000000000aa"

// alertEnv es un env con admin, viewer y un host con datos recientes en host_latest.
type alertEnv struct {
	*env
	admin, viewer *http.Cookie
}

func newAlertEnv(t *testing.T) *alertEnv {
	t.Helper()
	e := newEnv(t, Options{})
	admin := e.adminCookie()
	e.newUser("viewer", pwViewer, store.RoleViewer, false)
	return &alertEnv{env: e, admin: admin, viewer: e.login("viewer", pwViewer)}
}

// host enrola un host y le deja host_latest con cpu (ts = now() de la BD).
func (e *alertEnv) host(name string, cpu float64) string {
	e.t.Helper()
	id, _, _ := e.enrollHost(e.admin, name)
	mustExec(e.t, `INSERT INTO host_latest (host_id, ts, cpu_pct, mem_used_mb, mem_total_mb) VALUES ($1, now(), $2, 100, 200)
		ON CONFLICT (host_id) DO UPDATE SET ts = now(), cpu_pct = EXCLUDED.cpu_pct`, id, cpu)
	return id
}

func (e *alertEnv) eval() store.EvalResult {
	e.t.Helper()
	res, err := e.st.EvaluateAlerts(context.Background())
	if err != nil || !res.Ran {
		e.t.Fatalf("EvaluateAlerts got %+v, %v", res, err)
	}
	return res
}

func (e *alertEnv) createRule(body string) string {
	e.t.Helper()
	rec := e.do("POST", "/api/v1/alert-rules", body, withCookie(e.admin))
	e.wantStatus(rec, http.StatusCreated, "")
	var d struct {
		Rule struct {
			ID string `json:"id"`
		} `json:"rule"`
	}
	e.dataAs(rec, &d)
	return d.Rule.ID
}

const cpuRuleBody = `{"name":"CPU alta","kind":"metric","metric":"cpu_pct","op":"gt","threshold":90,"duration_seconds":0,"resolve_seconds":0,"severity":"critical"}`

type alertsPage struct {
	Items []struct {
		ID       string `json:"id"`
		State    string `json:"state"`
		Severity string `json:"severity"`
		RuleName string `json:"rule_name"`
		Host     struct {
			ID       string `json:"id"`
			Hostname string `json:"hostname"`
		} `json:"host"`
	} `json:"items"`
	NextCursor *string `json:"next_cursor"`
	Total      int     `json:"total"`
}

func (e *alertEnv) alerts(query string) alertsPage {
	e.t.Helper()
	rec := e.do("GET", "/api/v1/alerts"+query, "", withCookie(e.viewer))
	e.wantStatus(rec, http.StatusOK, "")
	var p alertsPage
	e.dataAs(rec, &p)
	return p
}

func TestAlertLifecycleThroughTheAPI(t *testing.T) {
	e := newAlertEnv(t)
	hostA := e.host("srv-a", 95)
	e.host("srv-b", 10)
	e.createRule(cpuRuleBody)

	if r := e.eval(); r.Opened != 1 {
		t.Fatalf("opened got %d, want 1", r.Opened)
	}
	p := e.alerts("")
	if p.Total != 1 || len(p.Items) != 1 || p.Items[0].Host.Hostname != "srv-a" || p.Items[0].State != "open" || p.Items[0].Host.ID != hostA {
		t.Fatalf("lista got %+v", p)
	}
	id := p.Items[0].ID

	// Detalle (viewer): alerta, entregas y eventos del host.
	rec := e.do("GET", "/api/v1/alerts/"+id, "", withCookie(e.viewer))
	e.wantStatus(rec, http.StatusOK, "")
	var det struct {
		Alert struct {
			ID           string   `json:"id"`
			TriggerValue *float64 `json:"trigger_value"`
			Kind         string   `json:"kind"`
		} `json:"alert"`
		Deliveries []json.RawMessage `json:"deliveries"`
		HostEvents []json.RawMessage `json:"host_events"`
	}
	e.dataAs(rec, &det)
	if det.Alert.ID != id || det.Alert.TriggerValue == nil || *det.Alert.TriggerValue != 95 || det.Deliveries == nil || det.HostEvents == nil {
		t.Errorf("detalle got %s", rec.Body.String())
	}

	// Resumen.
	rec = e.do("GET", "/api/v1/alerts/summary", "", withCookie(e.viewer))
	e.wantStatus(rec, http.StatusOK, "")
	var sum struct {
		Active     int `json:"active"`
		Suppressed int `json:"suppressed"`
		BySeverity map[string]struct {
			Open         int `json:"open"`
			Acknowledged int `json:"acknowledged"`
		} `json:"by_severity"`
	}
	e.dataAs(rec, &sum)
	if sum.Active != 1 || sum.BySeverity["critical"].Open != 1 || len(sum.BySeverity) != 3 {
		t.Errorf("resumen got %+v", sum)
	}

	// Viewer no puede reconocer ni resolver; admin si.
	e.wantStatus(e.do("POST", "/api/v1/alerts/"+id+"/ack", "", withCookie(e.viewer)), http.StatusForbidden, "forbidden")
	e.wantStatus(e.do("POST", "/api/v1/alerts/"+id+"/resolve", "", withCookie(e.viewer)), http.StatusForbidden, "forbidden")
	rec = e.do("POST", "/api/v1/alerts/"+id+"/ack", "", withCookie(e.admin))
	e.wantStatus(rec, http.StatusOK, "")
	var ack struct {
		Alert struct {
			State string  `json:"state"`
			By    *string `json:"acknowledged_by"`
		} `json:"alert"`
	}
	e.dataAs(rec, &ack)
	if ack.Alert.State != "acknowledged" || ack.Alert.By == nil || *ack.Alert.By != "admin" {
		t.Errorf("ack got %+v", ack)
	}
	if got := e.alerts("?state=acknowledged"); got.Total != 1 {
		t.Errorf("state=acknowledged total got %d", got.Total)
	}

	e.wantStatus(e.do("POST", "/api/v1/alerts/"+id+"/resolve", "", withCookie(e.admin)), http.StatusOK, "")
	e.wantStatus(e.do("POST", "/api/v1/alerts/"+id+"/resolve", "", withCookie(e.admin)), http.StatusConflict, "alert_resolved")
	e.wantStatus(e.do("POST", "/api/v1/alerts/"+id+"/ack", "", withCookie(e.admin)), http.StatusConflict, "alert_resolved")
	e.wantStatus(e.do("POST", "/api/v1/alerts/"+missingID+"/ack", "", withCookie(e.admin)), http.StatusNotFound, "not_found")
	e.wantStatus(e.do("POST", "/api/v1/alerts/"+missingID+"/resolve", "", withCookie(e.admin)), http.StatusNotFound, "not_found")
	e.wantStatus(e.do("GET", "/api/v1/alerts/"+missingID, "", withCookie(e.viewer)), http.StatusNotFound, "not_found")
	e.wantStatus(e.do("GET", "/api/v1/alerts/no-es-uuid", "", withCookie(e.viewer)), http.StatusBadRequest, "invalid_body")

	// Auditoria de ack y resolve: sin secretos, con las claves permitidas.
	for _, ev := range []string{"alert_acked", "alert_resolved"} {
		if n := e.count(`SELECT count(*) FROM audit_log WHERE event = $1 AND outcome = 'ok' AND target_id = $2`, ev, id); n != 1 {
			t.Errorf("auditoria de %s got %d, want 1", ev, n)
		}
	}
}

func TestListAlertsFiltersValidationAndKeyset(t *testing.T) {
	e := newAlertEnv(t)
	hosts := make([]string, 5)
	for i := range hosts {
		hosts[i] = e.host(fmt.Sprintf("srv-%d", i), 95)
	}
	e.createRule(cpuRuleBody)
	e.createRule(`{"name":"Mem","kind":"metric","metric":"mem_pct","op":"gt","threshold":10,"duration_seconds":0,"severity":"info"}`)
	if r := e.eval(); r.Opened != 10 {
		t.Fatalf("opened got %d, want 10", r.Opened)
	}

	if p := e.alerts("?severity=critical"); p.Total != 5 {
		t.Errorf("severity=critical total got %d, want 5", p.Total)
	}
	if p := e.alerts("?severity=critical,info"); p.Total != 10 {
		t.Errorf("severity OR total got %d, want 10", p.Total)
	}
	if p := e.alerts("?host_id=" + hosts[0] + "&host_id=" + hosts[1]); p.Total != 4 {
		t.Errorf("host_id repetido total got %d, want 4", p.Total)
	}
	if p := e.alerts("?state=resolved"); p.Total != 0 || len(p.Items) != 0 {
		t.Errorf("state=resolved got %+v", p)
	}

	seen := map[string]bool{}
	q := "?limit=3"
	pages := 0
	for {
		p := e.alerts(q)
		pages++
		if p.Total != 10 {
			t.Fatalf("total got %d, want 10", p.Total)
		}
		for _, it := range p.Items {
			if seen[it.ID] {
				t.Fatalf("alerta repetida entre paginas: %s", it.ID)
			}
			seen[it.ID] = true
		}
		if p.NextCursor == nil {
			break
		}
		q = "?limit=3&cursor=" + *p.NextCursor
	}
	if len(seen) != 10 || pages != 4 {
		t.Errorf("paginacion vio %d alertas en %d paginas, want 10 en 4", len(seen), pages)
	}

	// Cursor: de otra consulta, alterado o basura = 400; filtros invalidos = 400.
	first := e.alerts("?limit=3")
	other := "?severity=info&limit=3&cursor=" + *first.NextCursor
	for name, path := range map[string]string{
		"cursor de otra consulta": other,
		"cursor alterado":         "?limit=3&cursor=" + *first.NextCursor + "x",
		"cursor basura":           "?cursor=basura",
	} {
		rec := e.do("GET", "/api/v1/alerts"+path, "", withCookie(e.viewer))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_cursor") {
			t.Errorf("%s: got %d %s, want 400 invalid_cursor", name, rec.Code, rec.Body.String())
		}
	}
	for _, path := range []string{"?state=nope", "?severity=grave", "?host_id=1234", "?rule_id=x", "?limit=0", "?limit=99999", "?limit=abc"} {
		if rec := e.do("GET", "/api/v1/alerts"+path, "", withCookie(e.viewer)); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status got %d, want 400", path, rec.Code)
		}
	}
}

func TestAlertRuleCRUDAndValidation(t *testing.T) {
	e := newAlertEnv(t)
	id := e.createRule(cpuRuleBody)

	// Lectura (viewer) y forma del DTO.
	rec := e.do("GET", "/api/v1/alert-rules/"+id, "", withCookie(e.viewer))
	e.wantStatus(rec, http.StatusOK, "")
	var got struct {
		Rule struct {
			Name        string   `json:"name"`
			Enabled     bool     `json:"enabled"`
			ClearThresh *float64 `json:"clear_threshold"`
			Scope       struct {
				Type string `json:"type"`
			} `json:"scope"`
			ResolveSeconds int `json:"resolve_seconds"`
		} `json:"rule"`
	}
	e.dataAs(rec, &got)
	if got.Rule.Name != "CPU alta" || !got.Rule.Enabled || got.Rule.Scope.Type != "all" || got.Rule.ClearThresh != nil || got.Rule.ResolveSeconds != 0 {
		t.Errorf("regla got %s", rec.Body.String())
	}
	rec = e.do("GET", "/api/v1/alert-rules", "", withCookie(e.viewer))
	e.wantStatus(rec, http.StatusOK, "")
	var list struct {
		Items []struct{ ID string } `json:"items"`
	}
	e.dataAs(rec, &list)
	if len(list.Items) != 1 {
		t.Errorf("lista got %s", rec.Body.String())
	}

	g := e.newGroup(e.admin, `{"name":"prod"}`)
	bad := map[string]string{
		"nombre vacio":           `{"name":" ","kind":"host_status","duration_seconds":0,"severity":"info"}`,
		"nombre con control":     "{\"name\":\"a\\nb\",\"kind\":\"host_status\",\"duration_seconds\":0,\"severity\":\"info\"}",
		"nombre largo":           `{"name":"` + strings.Repeat("a", 81) + `","kind":"host_status","duration_seconds":0,"severity":"info"}`,
		"kind invalido":          `{"name":"x","kind":"otro","duration_seconds":0,"severity":"info"}`,
		"severity invalida":      `{"name":"x","kind":"host_status","duration_seconds":0,"severity":"grave"}`,
		"sin duracion":           `{"name":"x","kind":"host_status","severity":"info"}`,
		"duracion negativa":      `{"name":"x","kind":"host_status","duration_seconds":-1,"severity":"info"}`,
		"duracion enorme":        `{"name":"x","kind":"host_status","duration_seconds":86401,"severity":"info"}`,
		"resolve enorme":         `{"name":"x","kind":"host_status","duration_seconds":0,"resolve_seconds":86401,"severity":"info"}`,
		"metric sin metric":      `{"name":"x","kind":"metric","op":"gt","threshold":1,"duration_seconds":0,"severity":"info"}`,
		"metric invalida":        `{"name":"x","kind":"metric","metric":"net","op":"gt","threshold":1,"duration_seconds":0,"severity":"info"}`,
		"metric sin op":          `{"name":"x","kind":"metric","metric":"cpu_pct","threshold":1,"duration_seconds":0,"severity":"info"}`,
		"metric op invalido":     `{"name":"x","kind":"metric","metric":"cpu_pct","op":"eq","threshold":1,"duration_seconds":0,"severity":"info"}`,
		"metric sin threshold":   `{"name":"x","kind":"metric","metric":"cpu_pct","op":"gt","duration_seconds":0,"severity":"info"}`,
		"pct sobre 100":          `{"name":"x","kind":"metric","metric":"cpu_pct","op":"gt","threshold":100.5,"duration_seconds":0,"severity":"info"}`,
		"pct negativo":           `{"name":"x","kind":"metric","metric":"cpu_pct","op":"gt","threshold":-1,"duration_seconds":0,"severity":"info"}`,
		"load enorme":            `{"name":"x","kind":"metric","metric":"load1","op":"gt","threshold":1000001,"duration_seconds":0,"severity":"info"}`,
		"clear invertido gt":     `{"name":"x","kind":"metric","metric":"cpu_pct","op":"gt","threshold":90,"clear_threshold":95,"duration_seconds":0,"severity":"info"}`,
		"clear invertido lt":     `{"name":"x","kind":"metric","metric":"battery_pct","op":"lt","threshold":10,"clear_threshold":5,"duration_seconds":0,"severity":"info"}`,
		"clear fuera de rango":   `{"name":"x","kind":"metric","metric":"cpu_pct","op":"gt","threshold":90,"clear_threshold":-1,"duration_seconds":0,"severity":"info"}`,
		"host_status con metric": `{"name":"x","kind":"host_status","metric":"cpu_pct","duration_seconds":0,"severity":"info"}`,
		"host_status con umbral": `{"name":"x","kind":"host_status","threshold":5,"duration_seconds":0,"severity":"info"}`,
		"no_data sin umbral":     `{"name":"x","kind":"no_data","duration_seconds":0,"severity":"info"}`,
		"no_data umbral bajo":    `{"name":"x","kind":"no_data","threshold":30,"duration_seconds":0,"severity":"info"}`,
		"no_data con op":         `{"name":"x","kind":"no_data","threshold":120,"op":"gt","duration_seconds":0,"severity":"info"}`,
		"scope invalido":         `{"name":"x","kind":"host_status","duration_seconds":0,"severity":"info","scope":{"type":"planeta"}}`,
		"scope group sin id":     `{"name":"x","kind":"host_status","duration_seconds":0,"severity":"info","scope":{"type":"group"}}`,
		"scope group id malo":    `{"name":"x","kind":"host_status","duration_seconds":0,"severity":"info","scope":{"type":"group","group_id":"x"}}`,
		"scope all con grupo":    `{"name":"x","kind":"host_status","duration_seconds":0,"severity":"info","scope":{"type":"all","group_id":"` + g + `"}}`,
		"scope tag ya no vale":   `{"name":"x","kind":"host_status","duration_seconds":0,"severity":"info","scope":{"type":"tag","tag":"dev"}}`,
		"scope all con tag":      `{"name":"x","kind":"host_status","duration_seconds":0,"severity":"info","scope":{"type":"all","tag":"dev"}}`,
		"scope label ya no vale": `{"name":"x","kind":"host_status","duration_seconds":0,"severity":"info","scope":{"type":"label","label_key":"env","label_value":"dev"}}`,
		"campo desconocido":      `{"name":"x","kind":"host_status","duration_seconds":0,"severity":"info","admin":true}`,
		"json roto":              `{`,
	}
	for name, body := range bad {
		rec := e.do("POST", "/api/v1/alert-rules", body, withCookie(e.admin))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status got %d, want 400 (%s)", name, rec.Code, rec.Body.String())
		}
	}

	e.wantStatus(e.do("POST", "/api/v1/alert-rules", strings.Replace(cpuRuleBody, "CPU alta", "cpu ALTA", 1), withCookie(e.admin)), http.StatusConflict, "rule_exists")
	e.wantStatus(e.do("POST", "/api/v1/alert-rules",
		`{"name":"g","kind":"host_status","duration_seconds":0,"severity":"info","scope":{"type":"group","group_id":"`+missingID+`"}}`, withCookie(e.admin)),
		http.StatusUnprocessableEntity, "group_not_found")
	// Alcance por grupo y global validos.
	e.createRule(`{"name":"g","kind":"host_status","duration_seconds":120,"severity":"warn","enabled":false,"scope":{"type":"group","group_id":"` + g + `"}}`)
	e.createRule(`{"name":"l","kind":"no_data","threshold":300,"duration_seconds":0,"severity":"warn","scope":{"type":"all"}}`)

	// PATCH: parcial, kind/metric inmutables, clear_threshold null lo borra, validacion sobre el resultado fusionado.
	patch := func(path, body string) *httptest.ResponseRecorder {
		return e.do("PATCH", "/api/v1/alert-rules/"+path, body, withCookie(e.admin))
	}
	e.wantStatus(patch(id, `{"threshold":80,"clear_threshold":70,"enabled":false}`), http.StatusOK, "")
	rec = e.do("GET", "/api/v1/alert-rules/"+id, "", withCookie(e.viewer))
	e.dataAs(rec, &got)
	if got.Rule.Enabled || got.Rule.ClearThresh == nil || *got.Rule.ClearThresh != 70 {
		t.Errorf("tras patch got %s", rec.Body.String())
	}
	e.wantStatus(patch(id, `{"clear_threshold":null}`), http.StatusOK, "")
	e.dataAs(e.do("GET", "/api/v1/alert-rules/"+id, "", withCookie(e.viewer)), &got)
	if got.Rule.ClearThresh != nil {
		t.Error("clear_threshold null debe borrar la histeresis")
	}
	for name, body := range map[string]string{
		"kind inmutable":            `{"kind":"no_data"}`,
		"metric inmutable":          `{"metric":"mem_pct"}`,
		"vacio":                     `{}`,
		"umbral fuera de rango":     `{"threshold":101}`,
		"clear invertido fusionado": `{"threshold":50,"clear_threshold":60}`,
		"scope invalido":            `{"scope":{"type":"planeta"}}`,
		"nombre vacio":              `{"name":""}`,
	} {
		if rec := patch(id, body); rec.Code != http.StatusBadRequest {
			t.Errorf("patch %s: status got %d, want 400 (%s)", name, rec.Code, rec.Body.String())
		}
	}
	e.wantStatus(patch(id, `{"name":"g"}`), http.StatusConflict, "rule_exists")
	e.wantStatus(patch(missingID, `{"enabled":true}`), http.StatusNotFound, "not_found")
	e.wantStatus(patch("no-uuid", `{"enabled":true}`), http.StatusBadRequest, "invalid_body")
	// Una regla host_status no admite op via patch (la coherencia se valida sobre la regla completa).
	hs := e.createRule(`{"name":"hs","kind":"host_status","duration_seconds":0,"severity":"info"}`)
	e.wantStatus(patch(hs, `{"op":"gt"}`), http.StatusBadRequest, "invalid_body")

	// DELETE.
	e.wantStatus(e.do("DELETE", "/api/v1/alert-rules/"+id, "", withCookie(e.admin)), http.StatusOK, "")
	e.wantStatus(e.do("DELETE", "/api/v1/alert-rules/"+id, "", withCookie(e.admin)), http.StatusNotFound, "not_found")
	e.wantStatus(e.do("GET", "/api/v1/alert-rules/"+id, "", withCookie(e.viewer)), http.StatusNotFound, "not_found")
}

func TestViewerCannotWriteAnythingOfTheAlertingAPI(t *testing.T) {
	e := newAlertEnv(t)
	rule := e.createRule(cpuRuleBody)
	win := e.createWindow(`{"name":"m","scope":{"type":"all"}}`, time.Hour)
	before := e.count(`SELECT count(*) FROM alert_rules`)
	writes := []struct{ method, path, body string }{
		{"POST", "/api/v1/alert-rules", cpuRuleBody},
		{"PATCH", "/api/v1/alert-rules/" + rule, `{"enabled":false}`},
		{"DELETE", "/api/v1/alert-rules/" + rule, ""},
		{"POST", "/api/v1/alerts/" + missingID + "/ack", ""},
		{"POST", "/api/v1/alerts/" + missingID + "/resolve", ""},
		{"POST", "/api/v1/maintenance", `{"name":"x"}`},
		{"PATCH", "/api/v1/maintenance/" + win, `{"name":"y"}`},
		{"DELETE", "/api/v1/maintenance/" + win, ""},
	}
	for _, w := range writes {
		e.wantStatus(e.do(w.method, w.path, w.body, withCookie(e.viewer)), http.StatusForbidden, "forbidden")
	}
	if got := e.count(`SELECT count(*) FROM alert_rules`); got != before {
		t.Errorf("el viewer altero reglas: %d -> %d", before, got)
	}
	if got := e.count(`SELECT count(*) FROM maintenance_windows`); got != 1 {
		t.Errorf("el viewer altero ventanas: got %d", got)
	}
	// Las lecturas si.
	for _, p := range []string{"/api/v1/alerts", "/api/v1/alerts/summary", "/api/v1/alert-rules", "/api/v1/maintenance", "/api/v1/maintenance/" + win, "/api/v1/alert-rules/" + rule} {
		e.wantStatus(e.do("GET", p, "", withCookie(e.viewer)), http.StatusOK, "")
	}
}

// createWindow crea una ventana que empieza ahora y dura dur; body trae name y scope (sin fechas).
func (e *alertEnv) createWindow(body string, dur time.Duration) string {
	e.t.Helper()
	rec := e.do("POST", "/api/v1/maintenance", withDates(body, e.clock.Now(), dur), withCookie(e.admin))
	e.wantStatus(rec, http.StatusCreated, "")
	var d struct {
		Maintenance struct {
			ID string `json:"id"`
		} `json:"maintenance"`
	}
	e.dataAs(rec, &d)
	return d.Maintenance.ID
}

// withDates agrega starts_at (start) y ends_at (start+dur) a un cuerpo JSON de objeto.
func withDates(body string, start time.Time, dur time.Duration) string {
	return strings.TrimSuffix(body, "}") + fmt.Sprintf(`,"starts_at":%q,"ends_at":%q}`,
		start.UTC().Format(time.RFC3339), start.Add(dur).UTC().Format(time.RFC3339))
}

func TestMaintenanceCRUDAndValidation(t *testing.T) {
	e := newAlertEnv(t)
	now := e.clock.Now()
	g := e.newGroup(e.admin, `{"name":"prod"}`)

	post := func(body string, start time.Time, dur time.Duration) *httptest.ResponseRecorder {
		return e.do("POST", "/api/v1/maintenance", withDates(body, start, dur), withCookie(e.admin))
	}
	all := `{"name":"parche","scope":{"type":"all"}}`
	e.wantStatus(post(all, now, time.Hour), http.StatusCreated, "")
	e.wantStatus(post(all, now, 24*time.Hour), http.StatusCreated, "")
	tests := []struct {
		name  string
		body  string
		start time.Time
		dur   time.Duration
	}{
		{"all mas de 24 h", all, now, 24*time.Hour + time.Minute},
		{"mas de 31 dias", `{"name":"x","scope":{"type":"group","group_id":"` + g + `"}}`, now, 31*24*time.Hour + time.Minute},
		{"sin duracion", all, now, 0},
		{"fin anterior al inicio", all, now, -time.Hour},
		{"ya termino", `{"name":"x","scope":{"type":"group","group_id":"` + g + `"}}`, now.Add(-3 * time.Hour), time.Hour},
		{"inicio muy antiguo", `{"name":"x","scope":{"type":"group","group_id":"` + g + `"}}`, now.Add(-2 * time.Hour), 5 * time.Hour},
		{"inicio absurdo", `{"name":"x","scope":{"type":"group","group_id":"` + g + `"}}`, now.Add(365 * 24 * time.Hour), time.Hour},
		{"sin scope", `{"name":"x"}`, now, time.Hour},
		{"nombre vacio", `{"name":"","scope":{"type":"all"}}`, now, time.Hour},
		{"scope invalido", `{"name":"x","scope":{"type":"group"}}`, now, time.Hour},
	}
	for _, tt := range tests {
		if rec := post(tt.body, tt.start, tt.dur); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status got %d, want 400 (%s)", tt.name, rec.Code, rec.Body.String())
		}
	}
	e.wantStatus(e.do("POST", "/api/v1/maintenance", `{"name":"x","scope":{"type":"all"},"starts_at":"ayer","ends_at":"hoy"}`, withCookie(e.admin)), http.StatusBadRequest, "invalid_body")
	e.wantStatus(post(`{"name":"g","scope":{"type":"group","group_id":"`+missingID+`"}}`, now, time.Hour), http.StatusUnprocessableEntity, "group_not_found")
	// Grupo admite mas de 24 h.
	e.wantStatus(post(`{"name":"g","scope":{"type":"group","group_id":"`+g+`"}}`, now, 72*time.Hour), http.StatusCreated, "")
	id := e.createWindow(`{"name":"l","scope":{"type":"group","group_id":"`+g+`"}}`, 48*time.Hour)

	rec := e.do("GET", "/api/v1/maintenance", "", withCookie(e.viewer))
	e.wantStatus(rec, http.StatusOK, "")
	var list struct {
		Items []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Scope  struct {
				Type string `json:"type"`
			} `json:"scope"`
			CreatedBy *string `json:"created_by"`
		} `json:"items"`
	}
	e.dataAs(rec, &list)
	if len(list.Items) != 4 || list.Items[0].Status != "active" || list.Items[0].CreatedBy == nil || *list.Items[0].CreatedBy != "admin" {
		t.Errorf("lista got %s", rec.Body.String())
	}

	// PATCH: renombrar una ventana no revalida las fechas; cambiar el alcance a `all` con >24 h falla.
	patch := func(body string) *httptest.ResponseRecorder {
		return e.do("PATCH", "/api/v1/maintenance/"+id, body, withCookie(e.admin))
	}
	e.wantStatus(patch(`{"name":"otro"}`), http.StatusOK, "")
	e.wantStatus(patch(`{"scope":{"type":"all"}}`), http.StatusBadRequest, "invalid_body")
	e.wantStatus(patch(`{}`), http.StatusBadRequest, "invalid_body")
	e.wantStatus(patch(fmt.Sprintf(`{"ends_at":%q}`, now.Add(-time.Hour).UTC().Format(time.RFC3339))), http.StatusBadRequest, "invalid_body")
	e.wantStatus(patch(fmt.Sprintf(`{"ends_at":%q}`, now.Add(3*time.Hour).UTC().Format(time.RFC3339))), http.StatusOK, "")
	e.wantStatus(e.do("PATCH", "/api/v1/maintenance/"+missingID, `{"name":"x"}`, withCookie(e.admin)), http.StatusNotFound, "not_found")

	e.wantStatus(e.do("DELETE", "/api/v1/maintenance/"+id, "", withCookie(e.admin)), http.StatusOK, "")
	e.wantStatus(e.do("DELETE", "/api/v1/maintenance/"+id, "", withCookie(e.admin)), http.StatusNotFound, "not_found")
	e.wantStatus(e.do("GET", "/api/v1/maintenance/"+id, "", withCookie(e.viewer)), http.StatusNotFound, "not_found")

	// Tope de ventanas activas o programadas.
	mustExec(t, `DELETE FROM maintenance_windows`)
	mustExec(t, `INSERT INTO maintenance_windows (name, starts_at, ends_at, scope_type)
		SELECT 'w' || g, now(), now() + interval '1 hour', 'all' FROM generate_series(1, $1::int) g`, store.MaxActiveMaintenance)
	e.wantStatus(post(all, now, time.Hour), http.StatusConflict, "too_many_maintenance")
}

func TestMaintenanceSuppressesNotificationsButKeepsAlertsVisible(t *testing.T) {
	e := newAlertEnv(t)
	e.host("srv", 95)
	rule := e.createRule(cpuRuleBody)
	mustExec(t, `INSERT INTO notification_channels (id, name, kind) VALUES ('11111111-1111-4111-8111-111111111111', 'hook', 'webhook')`)
	mustExec(t, `INSERT INTO rule_channels (rule_id, channel_id) VALUES ($1, '11111111-1111-4111-8111-111111111111')`, rule)
	e.createWindow(`{"name":"parche","scope":{"type":"all"}}`, 2*time.Hour)

	e.eval()
	p := e.alerts("?state=open")
	if p.Total != 1 {
		t.Fatalf("la alerta en mantenimiento debe verse, got %+v", p)
	}
	var sum struct {
		Suppressed int `json:"suppressed"`
	}
	e.dataAs(e.do("GET", "/api/v1/alerts/summary", "", withCookie(e.viewer)), &sum)
	if sum.Suppressed != 1 {
		t.Errorf("summary.suppressed got %d, want 1", sum.Suppressed)
	}
	if n := e.count(`SELECT count(*) FROM notification_deliveries WHERE status = 'suppressed'`); n != 1 {
		t.Errorf("entregas suppressed got %d, want 1", n)
	}
	rec := e.do("GET", "/api/v1/alerts/"+p.Items[0].ID, "", withCookie(e.viewer))
	if !strings.Contains(rec.Body.String(), `"suppressed":true`) || !strings.Contains(rec.Body.String(), `"channel_name":"hook"`) {
		t.Errorf("detalle got %s", rec.Body.String())
	}
}

func TestHostEventsEndpoint(t *testing.T) {
	e := newAlertEnv(t)
	id := e.host("srv", 1)
	other := e.host("otro", 1)
	// Tres transiciones reales del host: detector, ingest, revocacion.
	mustExec(t, `INSERT INTO host_events (host_id, from_status, to_status, at, source) VALUES
		($1, 'up', 'offline', now() - interval '3 hours', 'detector'),
		($1, 'offline', 'up', now() - interval '2 hours', 'ingest')`, id)
	e.wantStatus(e.do("POST", "/api/v1/hosts/"+id+"/revoke", "", withCookie(e.admin)), http.StatusOK, "")

	type page struct {
		Items []struct {
			From   string `json:"from_status"`
			To     string `json:"to_status"`
			Source string `json:"source"`
		} `json:"items"`
		NextCursor *string `json:"next_cursor"`
	}
	get := func(path string, cookie *http.Cookie) (*httptest.ResponseRecorder, page) {
		rec := e.do("GET", path, "", withCookie(cookie))
		var p page
		if rec.Code == http.StatusOK {
			e.dataAs(rec, &p)
		}
		return rec, p
	}
	rec, p := get("/api/v1/hosts/"+id+"/events?limit=2", e.viewer)
	if rec.Code != http.StatusOK || len(p.Items) != 2 || p.NextCursor == nil || p.Items[0].To != "disabled" || p.Items[0].Source != "admin" {
		t.Fatalf("pagina 1 got %s", rec.Body.String())
	}
	rec, p2 := get("/api/v1/hosts/"+id+"/events?limit=2&cursor="+*p.NextCursor, e.viewer)
	if rec.Code != http.StatusOK || len(p2.Items) != 1 || p2.NextCursor != nil || p2.Items[0].Source != "detector" {
		t.Fatalf("pagina 2 got %s", rec.Body.String())
	}
	// El cursor de un host no sirve pa otro.
	e.wantStatus(e.do("GET", "/api/v1/hosts/"+other+"/events?cursor="+*p.NextCursor, "", withCookie(e.viewer)), http.StatusBadRequest, "invalid_cursor")
	e.wantStatus(e.do("GET", "/api/v1/hosts/"+missingID+"/events", "", withCookie(e.viewer)), http.StatusNotFound, "not_found")
	e.wantStatus(e.do("GET", "/api/v1/hosts/xyz/events", "", withCookie(e.viewer)), http.StatusBadRequest, "invalid_id")
	e.wantStatus(e.do("GET", "/api/v1/hosts/"+id+"/events?limit=0", "", withCookie(e.viewer)), http.StatusBadRequest, "invalid_body")
}

func TestAuditOfAlertingChangesOnlyKnownKeys(t *testing.T) {
	e := newAlertEnv(t)
	g := e.newGroup(e.admin, `{"name":"prod"}`)
	e.createRule(`{"name":"por grupo","kind":"no_data","threshold":300,"duration_seconds":0,"severity":"warn","scope":{"type":"group","group_id":"` + g + `"}}`)
	e.createWindow(`{"name":"vent","scope":{"type":"group","group_id":"`+g+`"}}`, time.Hour)
	rules := e.do("GET", "/api/v1/alert-rules", "", withCookie(e.admin))
	var list struct {
		Items []struct{ ID string } `json:"items"`
	}
	e.dataAs(rules, &list)
	e.wantStatus(e.do("PATCH", "/api/v1/alert-rules/"+list.Items[0].ID, `{"severity":"critical"}`, withCookie(e.admin)), http.StatusOK, "")
	e.wantStatus(e.do("DELETE", "/api/v1/alert-rules/"+list.Items[0].ID, "", withCookie(e.admin)), http.StatusOK, "")

	for _, ev := range []string{"alert_rule_created", "alert_rule_updated", "alert_rule_deleted", "maintenance_created"} {
		if n := e.count(`SELECT count(*) FROM audit_log WHERE event = $1 AND outcome = 'ok' AND actor_username = 'admin'`, ev); n != 1 {
			t.Errorf("auditoria de %s got %d, want 1", ev, n)
		}
	}
	var details string
	if err := testPool.QueryRow(context.Background(),
		`SELECT coalesce(string_agg(detail::text, ' '), '') FROM audit_log WHERE event LIKE 'alert_rule%' OR event LIKE 'maintenance%'`).Scan(&details); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(details, g) || strings.Contains(details, `"tag"`) {
		t.Errorf("la auditoria debe llevar el grupo en group_id y ninguna clave tag: %s", details)
	}
	if !strings.Contains(details, `"fields": ["severity"]`) && !strings.Contains(details, `"fields":["severity"]`) {
		t.Errorf("el patch debe auditar los campos cambiados: %s", details)
	}
}

func TestAlertRoutesAreRegisteredWithTheExpectedAccess(t *testing.T) {
	e := newEnv(t, Options{})
	want := map[string]access{
		"GET /api/v1/alerts": accessSession, "GET /api/v1/alerts/summary": accessSession, "GET /api/v1/alerts/{id}": accessSession,
		"POST /api/v1/alerts/{id}/ack": accessAdmin, "POST /api/v1/alerts/{id}/resolve": accessAdmin,
		"GET /api/v1/alert-rules": accessSession, "GET /api/v1/alert-rules/{id}": accessSession,
		"POST /api/v1/alert-rules": accessAdmin, "PATCH /api/v1/alert-rules/{id}": accessAdmin, "DELETE /api/v1/alert-rules/{id}": accessAdmin,
		"GET /api/v1/maintenance": accessSession, "GET /api/v1/maintenance/{id}": accessSession,
		"POST /api/v1/maintenance": accessAdmin, "PATCH /api/v1/maintenance/{id}": accessAdmin, "DELETE /api/v1/maintenance/{id}": accessAdmin,
		"GET /api/v1/hosts/{id}/events": accessSession,
	}
	got := map[string]access{}
	for _, rt := range e.api.routes {
		got[rt.pattern] = rt.access
	}
	for pattern, acc := range want {
		if g, ok := got[pattern]; !ok || g != acc {
			t.Errorf("ruta %q: acceso got %v (registrada %v), want %v", pattern, g, ok, acc)
		}
	}
}

func (f *fakeStore) ListAlerts(context.Context, store.Scope, store.AlertFilter) (store.AlertPage, error) {
	return store.AlertPage{}, f.x.err
}

func (f *fakeStore) AckAlert(context.Context, string, string, string) (store.Alert, error) {
	return store.Alert{}, f.x.err
}

// Un fallo de infraestructura es 500 con mensaje generico (el error real solo va al log); los errores de dominio
// se traducen en un unico sitio.
func TestAlertHandlersTranslateStoreErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		method     string
		path       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"caida de db al listar", "GET", "/api/v1/alerts", fmt.Errorf("ingest: %w", context.DeadlineExceeded), http.StatusInternalServerError, "internal_error"},
		{"db caida al reconocer", "POST", "/api/v1/alerts/" + missingID + "/ack", fmt.Errorf("conexion rota: password=hunter2"), http.StatusInternalServerError, "internal_error"},
		{"alerta inexistente", "POST", "/api/v1/alerts/" + missingID + "/ack", store.ErrNotFound, http.StatusNotFound, "not_found"},
		{"alerta ya resuelta", "POST", "/api/v1/alerts/" + missingID + "/ack", store.ErrAlertResolved, http.StatusConflict, "alert_resolved"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := &fakeStore{}
			fs.x.err = tt.err
			h, logs := newTestAPI(t, fs)
			rec, resp := do(t, h, tt.method, tt.path, "", "", asSession)
			wantError(t, rec, resp, tt.wantStatus, tt.wantCode)
			if tt.wantStatus == http.StatusInternalServerError {
				if strings.Contains(rec.Body.String(), "hunter2") || strings.Contains(rec.Body.String(), "deadline") {
					t.Errorf("el error real se filtro al cliente: %s", rec.Body.String())
				}
				if !strings.Contains(logs.String(), tt.err.Error()) {
					t.Errorf("el 500 no logueo el error real: %q", logs.String())
				}
			}
		})
	}
}
