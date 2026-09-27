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

	"argos/server/internal/notify"
	"argos/server/internal/store"
)

// Chequeos de servicio contra Postgres real: CRUD, secreto, entrega con checks_rev, ingest de resultados y alertas.

const (
	checkSecretValue = "Bearer TOKEN-ULTRA-SECRETO-DEL-CHEQUEO"
	checkURLWithQ    = "https://api.example.com/health?full=1"
)

type checkEnv struct {
	*alertEnv
	box *notify.KeyRing
}

func newCheckEnv(t *testing.T) *checkEnv {
	t.Helper()
	box, err := notify.NewKeyRing(channelKey, "")
	if err != nil {
		t.Fatal(err)
	}
	e := newEnv(t, Options{Secrets: box})
	admin := e.adminCookie()
	e.newUser("viewer", pwViewer, store.RoleViewer, false)
	return &checkEnv{alertEnv: &alertEnv{env: e, admin: admin, viewer: e.login("viewer", pwViewer)}, box: box}
}

func (e *checkEnv) create(body string) map[string]any {
	e.t.Helper()
	rec := e.do("POST", "/api/v1/service-checks", body, withCookie(e.admin))
	e.wantStatus(rec, http.StatusCreated, "")
	var d struct {
		Check map[string]any `json:"check"`
	}
	e.dataAs(rec, &d)
	return d.Check
}

func tcpBody(name string) string {
	return fmt.Sprintf(`{"name":%q,"type":"tcp","target_host":"localhost","target_port":22,"warn_ms":100,"crit_ms":500}`, name)
}

// httpBody es un chequeo http con secreto: siempre acotado a un grupo (con alcance all no se admite un secreto).
func httpBody(name, groupID string) string {
	return fmt.Sprintf(`{"name":%q,"type":"http","url":%q,"header_name":"Authorization","secret":%q,"scope":{"type":"group","group_id":%q}}`,
		name, checkURLWithQ, checkSecretValue, groupID)
}

func TestCheckSecretIsNeverReturnedNorAudited(t *testing.T) {
	e := newCheckEnv(t)
	rec := e.do("POST", "/api/v1/service-checks", httpBody("API salud", e.newGroup(e.admin, `{"name":"g-secreto"}`)), withCookie(e.admin))
	e.wantStatus(rec, http.StatusCreated, "")
	if strings.Contains(rec.Body.String(), "TOKEN-ULTRA") {
		t.Fatalf("crear devolvio el secreto: %s", rec.Body.String())
	}
	var d struct {
		Check map[string]any `json:"check"`
	}
	e.dataAs(rec, &d)
	id := d.Check["id"].(string)
	if d.Check["has_secret"] != true || d.Check["header_name"] != "Authorization" || d.Check["expect_status"] != "200-399" {
		t.Errorf("cheque creado got %v", d.Check)
	}

	var enc []byte
	var keyID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT secret_enc, secret_key_id FROM service_check_defs WHERE id = $1`, id).Scan(&enc, &keyID); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(enc), "TOKEN-ULTRA") {
		t.Error("el secreto quedo en claro en la BD")
	}
	raw, err := e.box.Open(id, keyID, "service_check", enc)
	if err != nil || string(raw) != checkSecretValue {
		t.Fatalf("el blob no abre con su chequeo: %q, %v", raw, err)
	}
	if _, err := e.box.Open(anyUUID, keyID, "service_check", enc); err == nil {
		t.Error("el blob no debe abrir con el id de otro chequeo (AAD)")
	}

	// Lecturas y PATCH: nunca el secreto; el viewer no ve header_name ni la query de la URL.
	for _, path := range []string{"/api/v1/service-checks", "/api/v1/service-checks/" + id} {
		for _, c := range []*http.Cookie{e.admin, e.viewer} {
			if body := e.do("GET", path, "", withCookie(c)).Body.String(); strings.Contains(body, "TOKEN-ULTRA") {
				t.Errorf("GET %s devolvio el secreto", path)
			}
		}
	}
	viewerBody := e.do("GET", "/api/v1/service-checks/"+id, "", withCookie(e.viewer)).Body.String()
	if strings.Contains(viewerBody, "header_name") || strings.Contains(viewerBody, "full=1") || !strings.Contains(viewerBody, `"has_secret":true`) {
		t.Errorf("el viewer no debe ver header_name ni la query: %s", viewerBody)
	}
	rec = e.do("PATCH", "/api/v1/service-checks/"+id, `{"secret":"nuevo-secreto-rotado","name":"API salud 2"}`, withCookie(e.admin))
	e.wantStatus(rec, http.StatusOK, "")
	if strings.Contains(rec.Body.String(), "rotado") {
		t.Error("PATCH devolvio el secreto")
	}

	// Auditoria: solo nombres de campo, jamas valores del secreto ni URL con query.
	var audit string
	if err := testPool.QueryRow(context.Background(),
		`SELECT coalesce(string_agg(detail::text, ' '), '') FROM audit_log WHERE event LIKE 'service_check_%'`).Scan(&audit); err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"TOKEN-ULTRA", "rotado", "full=1", "api.example.com", "Authorization"} {
		if strings.Contains(audit, leak) {
			t.Errorf("la auditoria filtro %q: %s", leak, audit)
		}
	}
	if !strings.Contains(audit, `"secret"`) || !strings.Contains(audit, `"type"`) {
		t.Errorf("la auditoria debe registrar el nombre del campo secret y el tipo: %s", audit)
	}
	if n := e.count(`SELECT count(*) FROM audit_log WHERE event = 'service_check_created' AND target_id = $1`, id); n != 1 {
		t.Errorf("auditoria de creacion got %d, want 1", n)
	}

	// secret null lo quita junto con la cabecera.
	rec = e.do("PATCH", "/api/v1/service-checks/"+id, `{"secret":null}`, withCookie(e.admin))
	e.wantStatus(rec, http.StatusOK, "")
	if n := e.count(`SELECT count(*) FROM service_check_defs WHERE id = $1 AND secret_enc IS NULL AND header_name IS NULL`, id); n != 1 {
		t.Error("secret null debe quitar secreto y cabecera")
	}
}

func TestCheckWritesAreAdminOnly(t *testing.T) {
	e := newCheckEnv(t)
	c := e.create(tcpBody("c"))
	id := c["id"].(string)
	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/api/v1/service-checks", tcpBody("otro")},
		{"PATCH", "/api/v1/service-checks/" + id, `{"enabled":false}`},
		{"DELETE", "/api/v1/service-checks/" + id, ""},
	} {
		e.wantStatus(e.do(tc.method, tc.path, tc.body, withCookie(e.viewer)), http.StatusForbidden, "forbidden")
	}
	if n := e.count(`SELECT count(*) FROM service_check_defs WHERE enabled`); n != 1 {
		t.Errorf("las escrituras del viewer no deben tener efecto, got %d", n)
	}
	e.wantStatus(e.do("GET", "/api/v1/service-checks", "", withCookie(e.viewer)), http.StatusOK, "")
}

func TestCheckValidation(t *testing.T) {
	e := newCheckEnv(t)
	g := e.newGroup(e.admin, `{"name":"g"}`)
	cases := []struct {
		name, body string
	}{
		{"tipo invalido", `{"name":"a","type":"ping","target_host":"h","target_port":1}`},
		{"campo desconocido", `{"name":"a","type":"tcp","target_host":"h","target_port":1,"comando":"ls"}`},
		{"tcp sin puerto", `{"name":"a","type":"tcp","target_host":"h"}`},
		{"host con caracteres raros", `{"name":"a","type":"tcp","target_host":"h;rm -rf","target_port":1}`},
		{"warn mayor que crit", `{"name":"a","type":"tcp","target_host":"h","target_port":1,"warn_ms":500,"crit_ms":100}`},
		{"timeout mayor que intervalo", `{"name":"a","type":"tcp","target_host":"h","target_port":1,"interval_seconds":30,"timeout_ms":31000}`},
		{"intervalo bajo el minimo", `{"name":"a","type":"tcp","target_host":"h","target_port":1,"interval_seconds":10}`},
		{"reintentos altos", `{"name":"a","type":"tcp","target_host":"h","target_port":1,"retries":4}`},
		{"url con credenciales", `{"name":"a","type":"http","url":"http://user:pass@h.example.com/"}`},
		{"url con token en query", `{"name":"a","type":"http","url":"http://h.example.com/x?api_key=abc"}`},
		{"url con fragmento", `{"name":"a","type":"http","url":"http://h.example.com/x#f"}`},
		{"url ftp", `{"name":"a","type":"http","url":"ftp://h.example.com/"}`},
		{"expect_status invalido", `{"name":"a","type":"http","url":"http://h.example.com/","expect_status":"2xx"}`},
		{"expect_status rango invertido", `{"name":"a","type":"http","url":"http://h.example.com/","expect_status":"399-200"}`},
		{"cabecera fuera de la lista", `{"name":"a","type":"http","url":"http://h.example.com/","header_name":"Cookie","secret":"x"}`},
		{"secret sin cabecera", `{"name":"a","type":"http","url":"http://h.example.com/","secret":"x"}`},
		{"cabecera sin secret", `{"name":"a","type":"http","url":"http://h.example.com/","header_name":"Authorization"}`},
		{"secret en tcp", `{"name":"a","type":"tcp","target_host":"h","target_port":1,"header_name":"Authorization","secret":"x"}`},
		{"tls crit >= warn", `{"name":"a","type":"tls_cert","target_host":"h","target_port":443,"warn_days":10,"crit_days":10}`},
		{"tls sin dias", `{"name":"a","type":"tls_cert","target_host":"h","target_port":443}`},
		{"campo de otro tipo", `{"name":"a","type":"tcp","target_host":"h","target_port":1,"service_name":"nginx"}`},
		{"servicio con nombre raro", `{"name":"a","type":"service","service_name":"nginx; reboot"}`},
		{"alcance grupo sin id", `{"name":"a","type":"tcp","target_host":"h","target_port":1,"scope":{"type":"group"}}`},
		{"nombre vacio", `{"name":" ","type":"tcp","target_host":"h","target_port":1}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e.wantStatus(e.do("POST", "/api/v1/service-checks", tc.body, withCookie(e.admin)), http.StatusBadRequest, "")
		})
	}
	if n := e.count(`SELECT count(*) FROM service_check_defs`); n != 0 {
		t.Fatalf("ninguna definicion invalida debe guardarse, got %d", n)
	}

	e.create(tcpBody("ok"))
	e.wantStatus(e.do("POST", "/api/v1/service-checks", tcpBody("OK"), withCookie(e.admin)), http.StatusConflict, "check_exists")
	e.wantStatus(e.do("POST", "/api/v1/service-checks",
		fmt.Sprintf(`{"name":"g2","type":"tcp","target_host":"h","target_port":1,"scope":{"type":"group","group_id":%q}}`, anyUUID),
		withCookie(e.admin)), http.StatusUnprocessableEntity, "group_not_found")
	e.create(fmt.Sprintf(`{"name":"g3","type":"service","service_name":"nginx.service","scope":{"type":"group","group_id":%q}}`, g))
	e.create(`{"name":"t","type":"tls_cert","target_host":"h","target_port":443,"warn_days":30,"crit_days":7,"scope":{"type":"all"}}`)
	// El alcance por etiqueta ya no existe.
	e.wantStatus(e.do("POST", "/api/v1/service-checks",
		`{"name":"etq","type":"tcp","target_host":"h","target_port":1,"scope":{"type":"tag","tag":"web"}}`, withCookie(e.admin)), http.StatusBadRequest, "")
}

func TestCheckPatchKeepsShapeConsistent(t *testing.T) {
	e := newCheckEnv(t)
	id := e.create(tcpBody("c"))["id"].(string)
	path := "/api/v1/service-checks/" + id
	e.wantStatus(e.do("PATCH", path, `{"type":"http"}`, withCookie(e.admin)), http.StatusBadRequest, "")
	e.wantStatus(e.do("PATCH", path, `{"url":"http://x.example.com/"}`, withCookie(e.admin)), http.StatusBadRequest, "")
	e.wantStatus(e.do("PATCH", path, `{"warn_ms":900}`, withCookie(e.admin)), http.StatusBadRequest, "") // 900 >= crit 500
	e.wantStatus(e.do("PATCH", path, `{"crit_ms":null,"warn_ms":900}`, withCookie(e.admin)), http.StatusOK, "")
	e.wantStatus(e.do("PATCH", path, `{}`, withCookie(e.admin)), http.StatusBadRequest, "")
	e.wantStatus(e.do("PATCH", "/api/v1/service-checks/"+anyUUID, `{"enabled":false}`, withCookie(e.admin)), http.StatusNotFound, "")
	e.wantStatus(e.do("DELETE", path, "", withCookie(e.admin)), http.StatusOK, "")
	e.wantStatus(e.do("DELETE", path, "", withCookie(e.admin)), http.StatusNotFound, "")
}

// --- entrega y resultados ---

type reportResp struct {
	Received bool `json:"received"`
	Checks   *struct {
		Rev   string           `json:"rev"`
		Items []map[string]any `json:"items"`
	} `json:"checks"`
}

// report manda un reporte del agente (avanza el reloj pa no chocar con el limitador) y devuelve la respuesta.
func (e *checkEnv) report(token, rev, services string) reportResp {
	e.t.Helper()
	e.clock.Advance(11 * time.Second)
	body := fmt.Sprintf(`{"metrics":{"cpu_pct":1},"checks_rev":%q,"services":[%s]}`, rev, services)
	rec := e.do("POST", "/api/v1/agent/metrics", body, withHeader("Authorization", "Bearer "+token))
	e.wantStatus(rec, http.StatusOK, "")
	var r reportResp
	e.dataAs(rec, &r)
	return r
}

func (e *checkEnv) reportRaw(token, services string) *httptest.ResponseRecorder {
	e.t.Helper()
	e.clock.Advance(11 * time.Second)
	body := fmt.Sprintf(`{"metrics":{"cpu_pct":1},"services":[%s]}`, services)
	return e.do("POST", "/api/v1/agent/metrics", body, withHeader("Authorization", "Bearer "+token))
}

func TestChecksDeliveryAndRevision(t *testing.T) {
	e := newCheckEnv(t)
	hostA, tokA, _ := e.enrollHost(e.admin, "host-a")
	_, tokB, _ := e.enrollHost(e.admin, "host-b")
	g := e.newGroup(e.admin, `{"name":"solo-a"}`)
	mustExec(t, `UPDATE hosts SET group_id = $1 WHERE id = $2`, g, hostA)

	all := e.create(tcpBody("todos"))
	httpc := e.create(httpBody("web", g))
	e.create(fmt.Sprintf(`{"name":"solo grupo","type":"service","service_name":"nginx","scope":{"type":"group","group_id":%q}}`, g))
	off := e.create(tcpBody("apagado"))
	e.wantStatus(e.do("PATCH", "/api/v1/service-checks/"+off["id"].(string), `{"enabled":false}`, withCookie(e.admin)), http.StatusOK, "")

	// Sin checks_rev (agente nuevo): recibe la lista completa, ordenada por id, con el secreto descifrado.
	first := e.report(tokA, "", "")
	if first.Checks == nil || len(first.Checks.Items) != 3 || len(first.Checks.Rev) != 16 {
		t.Fatalf("entrega inicial got %+v, want 3 chequeos y rev de 16 hex", first.Checks)
	}
	var prev string
	var sawSecret bool
	for _, it := range first.Checks.Items {
		id := it["id"].(string)
		if id < prev {
			t.Errorf("los chequeos deben ir ordenados por id: %s tras %s", id, prev)
		}
		prev = id
		if id == httpc["id"] {
			sawSecret = it["secret"] == checkSecretValue && it["header_name"] == "Authorization" && it["url"] == checkURLWithQ
		}
		if _, ok := it["enabled"]; ok {
			t.Error("la entrega no lleva enabled")
		}
	}
	if !sawSecret {
		t.Error("el agente dentro del alcance debe recibir el secreto descifrado, la cabecera y la URL")
	}
	// El otro host (sin grupo) no recibe el chequeo de grupo.
	if b := e.report(tokB, "", ""); b.Checks == nil || len(b.Checks.Items) != 1 {
		t.Errorf("host fuera del grupo got %+v, want 1 chequeo (el de alcance all)", b.Checks)
	}

	// Misma revision: se omite. Estable entre llamadas.
	if again := e.report(tokA, first.Checks.Rev, ""); again.Checks != nil {
		t.Errorf("con la misma revision no debe enviarse checks, got %+v", again.Checks)
	}
	if fresh := e.report(tokA, "", ""); fresh.Checks == nil || fresh.Checks.Rev != first.Checks.Rev {
		t.Errorf("la revision debe ser estable: %+v vs %s", fresh.Checks, first.Checks.Rev)
	}
	// Un agente con revision vieja/basura recibe la lista.
	if stale := e.report(tokA, "0000000000000000", ""); stale.Checks == nil {
		t.Error("revision distinta debe recibir la lista")
	}

	// Sensible a un cambio de campo.
	e.wantStatus(e.do("PATCH", "/api/v1/service-checks/"+all["id"].(string), `{"warn_ms":120}`, withCookie(e.admin)), http.StatusOK, "")
	r2 := e.report(tokA, first.Checks.Rev, "")
	if r2.Checks == nil || r2.Checks.Rev == first.Checks.Rev {
		t.Fatalf("un cambio de campo debe cambiar la revision: %+v", r2.Checks)
	}
	// Sensible a la rotacion del secreto (aunque el valor descifrado se conserve, el blob cambia).
	e.wantStatus(e.do("PATCH", "/api/v1/service-checks/"+httpc["id"].(string), `{"secret":"otro"}`, withCookie(e.admin)), http.StatusOK, "")
	r3 := e.report(tokA, r2.Checks.Rev, "")
	if r3.Checks == nil || r3.Checks.Rev == r2.Checks.Rev {
		t.Fatal("rotar el secreto debe cambiar la revision")
	}
	// Sensible al alcance resuelto: sacar el chequeo de grupo (alcance all -> group sin match) cambia la lista.
	// Y al desasignar el host del grupo.
	mustExec(t, `UPDATE hosts SET group_id = NULL WHERE id = $1`, hostA)
	r4 := e.report(tokA, r3.Checks.Rev, "")
	if r4.Checks == nil || len(r4.Checks.Items) != 1 || r4.Checks.Rev == r3.Checks.Rev {
		t.Fatalf("cambiar el alcance resuelto debe cambiar la lista y la revision: %+v", r4.Checks)
	}
}

func TestChecksDeliveryCapsCountAndBytes(t *testing.T) {
	e := newCheckEnv(t)
	_, tok, _ := e.enrollHost(e.admin, "h")
	mustExec(t, `INSERT INTO service_check_defs (name, type, target_host, target_port)
	              SELECT 'c' || g, 'tcp', 'localhost', 22 FROM generate_series(1, 60) g`)
	r := e.report(tok, "", "")
	if r.Checks == nil || len(r.Checks.Items) != store.MaxChecksPerHost {
		t.Fatalf("tope por host got %+v, want %d", r.Checks, store.MaxChecksPerHost)
	}
	if !strings.Contains(e.logs.String(), "truncados por tope") {
		t.Error("el truncado debe registrarse")
	}

	mustExec(t, `TRUNCATE service_check_defs CASCADE`)
	mustExec(t, `INSERT INTO service_check_defs (name, type, url)
	              SELECT 'w' || g, 'http', 'http://localhost/' || repeat('a', 480) FROM generate_series(1, 50) g`)
	r = e.report(tok, "", "")
	if r.Checks == nil || len(r.Checks.Items) == 0 || len(r.Checks.Items) >= 50 {
		t.Fatalf("el tope de bytes debe truncar la lista, got %+v", r.Checks)
	}
	b, err := json.Marshal(r.Checks.Items)
	if err != nil || len(b) > maxChecksPayloadBytes {
		t.Errorf("items serializados %d bytes (%v), want <= %d", len(b), err, maxChecksPayloadBytes)
	}
	if again := e.report(tok, r.Checks.Rev, ""); again.Checks != nil {
		t.Error("la revision de una lista truncada tambien debe ser estable")
	}
}

func TestCheckResultsIngestEndToEnd(t *testing.T) {
	e := newCheckEnv(t)
	hostA, tokA, _ := e.enrollHost(e.admin, "host-a")
	g := e.newGroup(e.admin, `{"name":"otro"}`)
	mine := e.create(tcpBody("mio"))["id"].(string)
	foreign := e.create(fmt.Sprintf(`{"name":"ajeno","type":"service","service_name":"nginx","scope":{"type":"group","group_id":%q}}`, g))["id"].(string)
	now := e.clock.Now().Unix()

	// Regla de alerta service_check por API.
	rec := e.do("POST", "/api/v1/alert-rules",
		fmt.Sprintf(`{"name":"mio caido","kind":"service_check","check_id":%q,"duration_seconds":0,"resolve_seconds":0,"severity":"critical"}`, mine),
		withCookie(e.admin))
	e.wantStatus(rec, http.StatusCreated, "")
	if !strings.Contains(rec.Body.String(), `"check_name":"mio"`) || !strings.Contains(rec.Body.String(), `"warn_or_down":false`) {
		t.Errorf("la regla debe mostrar check_name y warn_or_down: %s", rec.Body.String())
	}
	e.wantStatus(e.do("POST", "/api/v1/alert-rules",
		fmt.Sprintf(`{"name":"x","kind":"service_check","check_id":%q,"duration_seconds":0,"severity":"info"}`, anyUUID),
		withCookie(e.admin)), http.StatusUnprocessableEntity, "check_not_found")
	e.wantStatus(e.do("POST", "/api/v1/alert-rules",
		`{"name":"y","kind":"service_check","duration_seconds":0,"severity":"info"}`, withCookie(e.admin)), http.StatusBadRequest, "")
	e.wantStatus(e.do("POST", "/api/v1/alert-rules",
		fmt.Sprintf(`{"name":"z","kind":"host_status","check_id":%q,"duration_seconds":0,"severity":"info"}`, mine), withCookie(e.admin)), http.StatusBadRequest, "")

	svc := func(id, status, detail string, ts int64) string {
		return fmt.Sprintf(`{"check_id":%q,"name":"","status":%q,"response_ms":15,"value":null,"detail":%q,"ts":%d}`, id, status, detail, ts)
	}
	// Entradas legadas (sin check_id) conviven; el check_id ajeno se descarta.
	legacy := `{"name":"nginx","status":"up","response_ms":3}`
	e.report(tokA, "", strings.Join([]string{svc(mine, "down", "refused", now-30), svc(foreign, "down", "ok", now-30), legacy}, ","))
	if n := e.count(`SELECT count(*) FROM service_check_state`); n != 1 {
		t.Fatalf("estados got %d, want 1 (el ajeno se descarta)", n)
	}
	if n := e.count(`SELECT count(*) FROM service_checks WHERE name = 'nginx' AND check_id IS NULL`); n != 1 {
		t.Errorf("la entrada legada debe guardarse como muestra cruda, got %d", n)
	}

	// Estado visible por host.
	rec = e.do("GET", "/api/v1/hosts/"+hostA+"/service-checks", "", withCookie(e.viewer))
	e.wantStatus(rec, http.StatusOK, "")
	var items struct {
		Items []struct {
			Check struct {
				ID, Name, Type, Target string
				IntervalS              int `json:"interval_s"`
			} `json:"check"`
			State *struct {
				Status string
				Detail string
			} `json:"state"`
		} `json:"items"`
	}
	e.dataAs(rec, &items)
	if len(items.Items) != 1 || items.Items[0].Check.Target != "localhost:22" || items.Items[0].State == nil ||
		items.Items[0].State.Status != "down" || items.Items[0].State.Detail != "refused" {
		t.Fatalf("checks del host got %s", rec.Body.String())
	}

	// La alerta se abre.
	if res := e.eval(); res.Opened != 1 {
		t.Fatalf("opened got %d, want 1", res.Opened)
	}
	// Recupera: transicion y alerta resuelta.
	e.report(tokA, "", svc(mine, "up", "ok", now-10))
	if res := e.eval(); res.Resolved != 1 {
		t.Fatalf("resolved got %d, want 1", res.Resolved)
	}

	// Eventos con cursor.
	rec = e.do("GET", "/api/v1/hosts/"+hostA+"/service-checks/"+mine+"/events?limit=1", "", withCookie(e.viewer))
	e.wantStatus(rec, http.StatusOK, "")
	var page struct {
		Items []struct {
			ToStatus string `json:"to_status"`
		} `json:"items"`
		NextCursor *string `json:"next_cursor"`
	}
	e.dataAs(rec, &page)
	if len(page.Items) != 1 || page.Items[0].ToStatus != "up" || page.NextCursor == nil {
		t.Fatalf("eventos pagina 1 got %s", rec.Body.String())
	}
	rec = e.do("GET", "/api/v1/hosts/"+hostA+"/service-checks/"+mine+"/events?limit=1&cursor="+*page.NextCursor, "", withCookie(e.viewer))
	e.wantStatus(rec, http.StatusOK, "")
	e.dataAs(rec, &page)
	if len(page.Items) != 1 || page.Items[0].ToStatus != "down" {
		t.Fatalf("eventos pagina 2 got %s", rec.Body.String())
	}
	e.wantStatus(e.do("GET", "/api/v1/hosts/"+anyUUID+"/service-checks", "", withCookie(e.viewer)), http.StatusNotFound, "")
	e.wantStatus(e.do("GET", "/api/v1/hosts/"+hostA+"/service-checks/no-uuid/events", "", withCookie(e.viewer)), http.StatusBadRequest, "")
}

func TestCheckResultsValidation(t *testing.T) {
	e := newCheckEnv(t)
	_, tok, _ := e.enrollHost(e.admin, "h")
	id := e.create(tcpBody("c"))["id"].(string)
	now := e.clock.Now().Unix()
	mk := func(status, detail string, ms int, ts int64) string {
		return fmt.Sprintf(`{"check_id":%q,"status":%q,"response_ms":%d,"detail":%q,"ts":%d}`, id, status, ms, detail, ts)
	}
	bad := map[string]string{
		"status fuera del enum":     mk("broken", "ok", 1, now),
		"detail fuera del catalogo": mk("up", "connection to 10.0.0.5:22 refused", 1, now),
		"response_ms negativo":      mk("up", "ok", -1, now),
		"response_ms enorme":        mk("up", "ok", 700000, now),
		"check_id no uuid":          `{"check_id":"x","status":"up","ts":1}`,
		"sin ts":                    fmt.Sprintf(`{"check_id":%q,"status":"up"}`, id),
	}
	for name, svc := range bad {
		if rec := e.reportRaw(tok, svc); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status got %d, want 400 (%s)", name, rec.Code, rec.Body.String())
		}
	}
	if n := e.count(`SELECT count(*) FROM service_check_state`); n != 0 {
		t.Fatalf("resultados invalidos no deben dejar estado, got %d", n)
	}
	// ts absurdo (mas viejo que la ventana): se descarta sin rechazar el reporte.
	if rec := e.reportRaw(tok, mk("up", "ok", 1, now-30*24*3600)); rec.Code != http.StatusOK {
		t.Errorf("ts viejo: status got %d, want 200", rec.Code)
	}
	if n := e.count(`SELECT count(*) FROM service_check_state`); n != 0 {
		t.Errorf("un ts fuera de ventana se descarta, estados got %d", n)
	}
	if rec := e.reportRaw(tok, mk("unknown", "unsupported", 0, now-5)); rec.Code != http.StatusOK {
		t.Errorf("resultado valido: status got %d", rec.Code)
	}
	if n := e.count(`SELECT count(*) FROM service_check_state WHERE status = 'unknown' AND detail = 'unsupported'`); n != 1 {
		t.Errorf("estado valido got %d, want 1", n)
	}
}
