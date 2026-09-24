package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"argos/server/internal/notify"
	"argos/server/internal/store"
)

// API de canales contra Postgres real, con la clave de secretos real y un cliente webhook falso.

const (
	hookURL    = "https://hooks.example.com/services/T000/B000/TOKEN-ULTRA-SECRETO"
	hookHMAC   = "hmac-ultra-secreto"
	channelKey = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=" // 32 bytes
)

// fakeWebhook implementa WebhookClient y notify.Sender: registra lo enviado y devuelve los errores configurados.
type fakeWebhook struct {
	mu       sync.Mutex
	checkErr error
	sendErr  error
	code     int
	checked  []string
	sent     []notify.Target
	bodies   [][]byte
}

func (f *fakeWebhook) CheckURL(_ context.Context, raw string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checked = append(f.checked, raw)
	return f.checkErr
}

func (f *fakeWebhook) Send(_ context.Context, t notify.Target, body []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent, f.bodies = append(f.sent, t), append(f.bodies, body)
	return f.code, f.sendErr
}

type channelEnv struct {
	*alertEnv
	box  *notify.Box
	hook *fakeWebhook
}

func newChannelEnv(t *testing.T) *channelEnv {
	t.Helper()
	box, err := notify.NewBox(channelKey)
	if err != nil {
		t.Fatal(err)
	}
	hook := &fakeWebhook{code: 200}
	e := newEnv(t, Options{Secrets: box, Webhook: hook})
	admin := e.adminCookie()
	e.newUser("viewer", pwViewer, store.RoleViewer, false)
	return &channelEnv{alertEnv: &alertEnv{env: e, admin: admin, viewer: e.login("viewer", pwViewer)}, box: box, hook: hook}
}

func (e *channelEnv) createChannel(body string) string {
	e.t.Helper()
	rec := e.do("POST", "/api/v1/channels", body, withCookie(e.admin))
	e.wantStatus(rec, http.StatusCreated, "")
	var d struct {
		Channel struct {
			ID string `json:"id"`
		} `json:"channel"`
	}
	e.dataAs(rec, &d)
	return d.Channel.ID
}

func chBody(name string) string {
	return fmt.Sprintf(`{"name":%q,"kind":"webhook","url":%q,"hmac_secret":%q,"min_severity":"info"}`, name, hookURL, hookHMAC)
}

func TestChannelCreateNeverEchoesOrStoresTheSecretInTheClear(t *testing.T) {
	e := newChannelEnv(t)
	rec := e.do("POST", "/api/v1/channels", chBody("Slack ops"), withCookie(e.admin))
	e.wantStatus(rec, http.StatusCreated, "")
	for _, secret := range []string{"TOKEN-ULTRA-SECRETO", hookHMAC, "hooks.example.com/services"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Errorf("la respuesta de crear devolvio %q: %s", secret, rec.Body.String())
		}
	}
	var created struct {
		Channel map[string]any `json:"channel"`
	}
	e.dataAs(rec, &created)
	id := created.Channel["id"].(string)
	if created.Channel["has_secret"] != true || created.Channel["min_severity"] != "info" || created.Channel["enabled"] != true {
		t.Errorf("canal creado got %v", created.Channel)
	}
	cfg, _ := created.Channel["config"].(map[string]any)
	if cfg["host"] != "hooks.example.com" || cfg["signed"] != true || len(cfg) != 2 {
		t.Errorf("el admin ve solo host y si firma: %v", cfg)
	}

	// En la BD: cifrado, con la clave y AAD correctos, y la config sin el token.
	var enc []byte
	var keyID string
	var config string
	if err := testPool.QueryRow(context.Background(),
		`SELECT secret_enc, secret_key_id, config::text FROM notification_channels WHERE id = $1`, id).Scan(&enc, &keyID, &config); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(enc), "TOKEN-ULTRA-SECRETO") || strings.Contains(config, "TOKEN") || strings.Contains(config, "/services") {
		t.Error("el secreto quedo en claro en la BD")
	}
	raw, err := e.box.Open(id, keyID, "webhook", enc)
	if err != nil {
		t.Fatalf("el blob no abre con su canal: %v", err)
	}
	var sec notify.WebhookSecret
	if err := json.Unmarshal(raw, &sec); err != nil || sec.URL != hookURL || sec.HMAC != hookHMAC {
		t.Errorf("secreto guardado got %+v, %v", sec, err)
	}
	if _, err := e.box.Open("00000000-0000-4000-8000-000000000001", keyID, "webhook", enc); err == nil {
		t.Error("el blob no debe abrir con el id de otro canal (AAD)")
	}
	if !slices.Contains(e.hook.checked, hookURL) {
		t.Error("la URL debe validarse con la politica ANTES de guardarla")
	}
}

func TestViewerSeesChannelsWithoutConfigOrDestination(t *testing.T) {
	e := newChannelEnv(t)
	id := e.createChannel(chBody("Slack ops"))

	for _, path := range []string{"/api/v1/channels", "/api/v1/channels/" + id} {
		rec := e.do("GET", path, "", withCookie(e.viewer))
		e.wantStatus(rec, http.StatusOK, "")
		for _, leak := range []string{"config", "hooks.example.com", "TOKEN", "hmac", "signed", "host", "url", "created_at"} {
			if strings.Contains(rec.Body.String(), leak) {
				t.Errorf("GET %s como viewer filtra %q: %s", path, leak, rec.Body.String())
			}
		}
	}
	var one struct {
		Channel map[string]any `json:"channel"`
	}
	e.dataAs(e.do("GET", "/api/v1/channels/"+id, "", withCookie(e.viewer)), &one)
	keys := make([]string, 0, len(one.Channel))
	for k := range one.Channel {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if want := []string{"enabled", "has_secret", "id", "kind", "min_severity", "name"}; !slices.Equal(keys, want) {
		t.Errorf("campos del viewer got %v, want %v", keys, want)
	}

	// El admin si ve config (host, nunca la URL).
	adminRec := e.do("GET", "/api/v1/channels", "", withCookie(e.admin))
	if !strings.Contains(adminRec.Body.String(), `"host":"hooks.example.com"`) || strings.Contains(adminRec.Body.String(), "TOKEN") {
		t.Errorf("lista del admin: %s", adminRec.Body.String())
	}
}

func TestViewerGets403OnEveryChannelWrite(t *testing.T) {
	e := newChannelEnv(t)
	id := e.createChannel(chBody("Slack ops"))
	ruleID := e.createRule(cpuRuleBody)
	before := len(e.hook.sent)

	writes := []struct{ method, path, body string }{
		{"POST", "/api/v1/channels", chBody("otro")},
		{"PATCH", "/api/v1/channels/" + id, `{"enabled":false}`},
		{"DELETE", "/api/v1/channels/" + id, ""},
		{"POST", "/api/v1/channels/" + id + "/test", ""},
		{"PUT", "/api/v1/alert-rules/" + ruleID + "/channels", `{"channel_ids":[]}`},
	}
	for _, w := range writes {
		e.wantStatus(e.do(w.method, w.path, w.body, withCookie(e.viewer)), http.StatusForbidden, "forbidden")
		e.wantStatus(e.do(w.method, w.path, w.body), http.StatusUnauthorized, "unauthenticated")
	}
	if len(e.hook.sent) != before {
		t.Error("un viewer disparo un envio de prueba")
	}
	if n := e.count(`SELECT count(*) FROM notification_channels`); n != 1 {
		t.Errorf("canales got %d, want 1 (nada cambio)", n)
	}
}

func TestChannelValidationAndErrors(t *testing.T) {
	e := newChannelEnv(t)
	e.createChannel(chBody("Existente"))
	tests := []struct {
		name   string
		body   string
		hook   error
		status int
		code   string
	}{
		{"sin nombre", `{"kind":"webhook","url":"` + hookURL + `"}`, nil, 400, "invalid_body"},
		{"nombre con control", `{"name":"a\u0000b","kind":"webhook","url":"` + hookURL + `"}`, nil, 400, "invalid_body"},
		{"kind email aun no", `{"name":"x","kind":"email","url":"` + hookURL + `"}`, nil, 400, "invalid_body"},
		{"severidad invalida", `{"name":"x","kind":"webhook","url":"` + hookURL + `","min_severity":"urgente"}`, nil, 400, "invalid_body"},
		{"campo desconocido", `{"name":"x","kind":"webhook","url":"` + hookURL + `","config":{"a":1}}`, nil, 400, "invalid_body"},
		{"hmac gigante", `{"name":"x","kind":"webhook","url":"` + hookURL + `","hmac_secret":"` + strings.Repeat("a", 300) + `"}`, nil, 400, "invalid_body"},
		{"url rechazada por la politica", chBody("x"), fmt.Errorf("%w: el esquema debe ser https", notify.ErrURLInvalid), 400, "invalid_body"},
		{"destino interno", chBody("x"), notify.ErrBlockedDestination, 400, "invalid_body"},
		{"nombre repetido", chBody("existente"), nil, 409, "channel_exists"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e.hook.checkErr = tt.hook
			rec := e.do("POST", "/api/v1/channels", tt.body, withCookie(e.admin))
			e.wantStatus(rec, tt.status, tt.code)
			for _, leak := range []string{"TOKEN-ULTRA-SECRETO", "hooks.example.com"} {
				if strings.Contains(rec.Body.String(), leak) {
					t.Errorf("el error repite la URL: %s", rec.Body.String())
				}
			}
		})
	}
	e.hook.checkErr = fmt.Errorf("resolviendo: %w", context.DeadlineExceeded)
	rec := e.do("POST", "/api/v1/channels", chBody("otro"), withCookie(e.admin))
	e.wantStatus(rec, http.StatusInternalServerError, "internal_error")
	if strings.Contains(rec.Body.String(), "deadline") {
		t.Errorf("un fallo de infraestructura no debe llegar al cliente: %s", rec.Body.String())
	}
}

func TestChannelsNeedTheSecretKey(t *testing.T) {
	e := newEnv(t, Options{}) // sin ARGOS_SECRET_KEY
	admin := e.adminCookie()
	e.wantStatus(e.do("POST", "/api/v1/channels", chBody("x"), withCookie(admin)), http.StatusServiceUnavailable, "secret_key_missing")
	// Lo que no toca el secreto sigue funcionando.
	id := "00000000-0000-4000-8000-000000000001"
	mustExec(e.t, `INSERT INTO notification_channels (id, name, kind, secret_enc, secret_key_id) VALUES ($1, 'x', 'webhook', '\x00'::bytea, 'k')`, id)
	e.wantStatus(e.do("PATCH", "/api/v1/channels/"+id, `{"enabled":false}`, withCookie(admin)), http.StatusOK, "")
	e.wantStatus(e.do("PATCH", "/api/v1/channels/"+id, `{"url":"`+hookURL+`"}`, withCookie(admin)), http.StatusServiceUnavailable, "secret_key_missing")
	e.wantStatus(e.do("POST", "/api/v1/channels/"+id+"/test", "", withCookie(admin)), http.StatusServiceUnavailable, "secret_key_missing")
}

func TestChannelUpdateReencryptsAndKeepsWhatIsNotChanged(t *testing.T) {
	e := newChannelEnv(t)
	id := e.createChannel(chBody("Slack ops"))
	secret := func() notify.WebhookSecret {
		var enc []byte
		var keyID string
		if err := testPool.QueryRow(context.Background(), `SELECT secret_enc, secret_key_id FROM notification_channels WHERE id = $1`, id).Scan(&enc, &keyID); err != nil {
			t.Fatal(err)
		}
		raw, err := e.box.Open(id, keyID, "webhook", enc)
		if err != nil {
			t.Fatal(err)
		}
		var s notify.WebhookSecret
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	patch := func(body string) {
		rec := e.do("PATCH", "/api/v1/channels/"+id, body, withCookie(e.admin))
		e.wantStatus(rec, http.StatusOK, "")
	}

	patch(`{"name":"Slack renombrado","min_severity":"critical","enabled":false}`)
	if s := secret(); s.URL != hookURL || s.HMAC != hookHMAC {
		t.Errorf("un patch sin secretos no debe tocarlos: %+v", s)
	}
	const newURL = "https://hooks.example.com/services/OTRO-TOKEN"
	patch(`{"url":"` + newURL + `"}`)
	if s := secret(); s.URL != newURL || s.HMAC != hookHMAC {
		t.Errorf("cambiar la url conserva el hmac: %+v", s)
	}
	patch(`{"hmac_secret":"nuevo-hmac"}`)
	if s := secret(); s.URL != newURL || s.HMAC != "nuevo-hmac" {
		t.Errorf("cambiar el hmac conserva la url: %+v", s)
	}
	patch(`{"hmac_secret":null}`)
	if s := secret(); s.HMAC != "" {
		t.Errorf("hmac_secret null debe quitar la firma: %+v", s)
	}
	var cfg string
	if err := testPool.QueryRow(context.Background(), `SELECT config::text FROM notification_channels WHERE id = $1`, id).Scan(&cfg); err != nil || !strings.Contains(cfg, `"signed": false`) {
		t.Errorf("config got %s, %v", cfg, err)
	}

	e.hook.checkErr = notify.ErrBlockedDestination
	e.wantStatus(e.do("PATCH", "/api/v1/channels/"+id, `{"url":"https://10.0.0.1/x"}`, withCookie(e.admin)), http.StatusBadRequest, "invalid_body")
	e.hook.checkErr = nil
	e.wantStatus(e.do("PATCH", "/api/v1/channels/"+id, `{}`, withCookie(e.admin)), http.StatusBadRequest, "invalid_body")
	e.wantStatus(e.do("PATCH", "/api/v1/channels/"+id, `{"kind":"email"}`, withCookie(e.admin)), http.StatusBadRequest, "invalid_body")
	e.wantStatus(e.do("PATCH", "/api/v1/channels/"+missingID, `{"enabled":true}`, withCookie(e.admin)), http.StatusNotFound, "not_found")
	if s := secret(); s.URL != newURL {
		t.Error("un patch rechazado no debe cambiar el secreto")
	}

	e.wantStatus(e.do("DELETE", "/api/v1/channels/"+id, "", withCookie(e.admin)), http.StatusOK, "")
	e.wantStatus(e.do("GET", "/api/v1/channels/"+id, "", withCookie(e.admin)), http.StatusNotFound, "not_found")
}

func TestChannelTestEndpointReportsOnlyOkAndACategory(t *testing.T) {
	e := newChannelEnv(t)
	id := e.createChannel(chBody("Slack ops"))
	call := func() (int, map[string]any) {
		rec := e.do("POST", "/api/v1/channels/"+id+"/test", "", withCookie(e.admin))
		var d map[string]any
		if rec.Code == http.StatusOK {
			e.dataAs(rec, &d)
		}
		return rec.Code, d
	}

	_, ok := call()
	if ok["ok"] != true || ok["category"] != nil || len(ok) != 2 {
		t.Errorf("exito got %v, want {ok:true, category:null}", ok)
	}
	if got := e.hook.sent[len(e.hook.sent)-1]; got.URL != hookURL || got.HMACSecret != hookHMAC {
		t.Errorf("la prueba debe usar el secreto guardado: %+v", got)
	}
	if !strings.Contains(string(e.hook.bodies[len(e.hook.bodies)-1]), `"kind":"test"`) {
		t.Error("el cuerpo de la prueba debe ser el mensaje de prueba")
	}

	for _, tt := range []struct {
		name string
		err  error
		want string
	}{
		{"status", &notify.SendError{Category: notify.CatStatus, HTTPStatus: 502}, "status"},
		{"dns", &notify.SendError{Category: notify.CatDNS}, "dns"},
		{"tls", &notify.SendError{Category: notify.CatTLS}, "tls"},
		{"timeout", &notify.SendError{Category: notify.CatTimeout}, "timeout"},
		{"bloqueado se reporta como connect", &notify.SendError{Category: notify.CatConnect, Blocked: true}, "connect"},
		{"error desconocido", fmt.Errorf("dial tcp 10.0.0.1:443: refused"), "connect"},
	} {
		e.clock.Advance(2 * time.Minute) // fuera de la ventana del limite (5/min)
		e.hook.sendErr = tt.err
		status, d := call()
		if status != http.StatusOK || d["ok"] != false || d["category"] != tt.want || len(d) != 2 {
			t.Errorf("%s: got %d %v, want ok=false category=%s y solo esas 2 claves", tt.name, status, d, tt.want)
		}
	}
	e.wantStatus(e.do("POST", "/api/v1/channels/"+missingID+"/test", "", withCookie(e.admin)), http.StatusNotFound, "not_found")

	// Cada uso queda auditado (ok y fail), sin secretos ni la URL.
	if n := e.count(`SELECT count(*) FROM audit_log WHERE event = 'channel_tested' AND outcome = 'ok'`); n != 1 {
		t.Errorf("auditorias ok got %d, want 1", n)
	}
	if n := e.count(`SELECT count(*) FROM audit_log WHERE event = 'channel_tested' AND outcome = 'fail' AND detail->>'reason' IS NOT NULL`); n != 6 {
		t.Errorf("auditorias fail got %d, want 6", n)
	}
}

func TestChannelTestEndpointIsRateLimitedPerUser(t *testing.T) {
	e := newChannelEnv(t)
	id := e.createChannel(chBody("Slack ops"))
	e.newUser("admin2", pwViewer, store.RoleAdmin, false)
	admin2 := e.login("admin2", pwViewer)

	for range channelTestsPerMinute {
		e.wantStatus(e.do("POST", "/api/v1/channels/"+id+"/test", "", withCookie(e.admin)), http.StatusOK, "")
	}
	rec := e.do("POST", "/api/v1/channels/"+id+"/test", "", withCookie(e.admin))
	e.wantStatus(rec, http.StatusTooManyRequests, "rate_limited")
	if rec.Header().Get("Retry-After") == "" {
		t.Error("falta Retry-After")
	}
	if got := len(e.hook.sent); got != channelTestsPerMinute {
		t.Errorf("envios got %d, want %d (el 429 no envia)", got, channelTestsPerMinute)
	}
	// El limite es por usuario: otro admin no queda bloqueado.
	e.wantStatus(e.do("POST", "/api/v1/channels/"+id+"/test", "", withCookie(admin2)), http.StatusOK, "")
}

func TestRuleChannelAssignmentContract(t *testing.T) {
	e := newChannelEnv(t)
	a, b := e.createChannel(chBody("a")), e.createChannel(chBody("b"))
	ruleID := e.createRule(cpuRuleBody)
	put := func(body string) *httptest.ResponseRecorder {
		return e.do("PUT", "/api/v1/alert-rules/"+ruleID+"/channels", body, withCookie(e.admin))
	}

	rec := put(fmt.Sprintf(`{"channel_ids":[%q,%q,%q]}`, b, a, strings.ToUpper(a)))
	e.wantStatus(rec, http.StatusOK, "")
	var res struct {
		ChannelIDs []string `json:"channel_ids"`
	}
	e.dataAs(rec, &res)
	want := []string{a, b}
	slices.Sort(want)
	if !slices.Equal(res.ChannelIDs, want) {
		t.Errorf("channel_ids got %v, want %v (sin duplicados)", res.ChannelIDs, want)
	}

	// El GET de la regla y el listado traen channel_ids (tambien pa el viewer).
	var got struct {
		Rule struct {
			ChannelIDs []string `json:"channel_ids"`
		} `json:"rule"`
	}
	e.dataAs(e.do("GET", "/api/v1/alert-rules/"+ruleID, "", withCookie(e.viewer)), &got)
	if !slices.Equal(got.Rule.ChannelIDs, want) {
		t.Errorf("GET regla channel_ids got %v", got.Rule.ChannelIDs)
	}
	if !strings.Contains(e.do("GET", "/api/v1/alert-rules", "", withCookie(e.viewer)).Body.String(), `"channel_ids":["`) {
		t.Error("el listado de reglas debe incluir channel_ids")
	}

	e.wantStatus(put(`{"channel_ids":["00000000-0000-4000-8000-0000000000ff"]}`), http.StatusUnprocessableEntity, "channel_not_found")
	e.wantStatus(put(`{}`), http.StatusBadRequest, "invalid_body")
	e.wantStatus(put(`{"channel_ids":["no-es-uuid"]}`), http.StatusBadRequest, "invalid_body")
	e.wantStatus(put(`{"channel_ids":[],"otro":1}`), http.StatusBadRequest, "invalid_body")
	e.wantStatus(e.do("PUT", "/api/v1/alert-rules/"+missingID+"/channels", `{"channel_ids":[]}`, withCookie(e.admin)), http.StatusNotFound, "not_found")

	e.wantStatus(put(`{"channel_ids":[]}`), http.StatusOK, "")
	e.dataAs(e.do("GET", "/api/v1/alert-rules/"+ruleID, "", withCookie(e.admin)), &got)
	if len(got.Rule.ChannelIDs) != 0 || got.Rule.ChannelIDs == nil {
		t.Errorf("sin canales debe ser lista vacia: %#v", got.Rule.ChannelIDs)
	}
	if n := e.count(`SELECT count(*) FROM audit_log WHERE event = 'alert_rule_channels_set' AND outcome = 'ok'`); n != 2 {
		t.Errorf("auditorias de asignacion got %d, want 2", n)
	}
}

func TestChannelAuditHasNoSecretsNorFullURLs(t *testing.T) {
	e := newChannelEnv(t)
	id := e.createChannel(chBody("Slack ops"))
	e.wantStatus(e.do("PATCH", "/api/v1/channels/"+id, `{"url":"https://hooks.example.com/services/OTRO-TOKEN","hmac_secret":"otro-hmac","enabled":false}`, withCookie(e.admin)), http.StatusOK, "")
	e.wantStatus(e.do("PATCH", "/api/v1/channels/"+id, `{"name":"Renombrado"}`, withCookie(e.admin)), http.StatusOK, "")
	e.wantStatus(e.do("POST", "/api/v1/channels/"+id+"/test", "", withCookie(e.admin)), http.StatusOK, "")
	e.wantStatus(e.do("DELETE", "/api/v1/channels/"+id, "", withCookie(e.admin)), http.StatusOK, "")

	for _, ev := range []string{"channel_created", "channel_updated", "channel_tested", "channel_deleted"} {
		if n := e.count(`SELECT count(*) FROM audit_log WHERE event = $1 AND actor_username = 'admin'`, ev); n < 1 {
			t.Errorf("falta la auditoria de %s", ev)
		}
	}
	var details string
	if err := testPool.QueryRow(context.Background(),
		`SELECT coalesce(string_agg(coalesce(detail::text, '') || ' ' || coalesce(target_id, ''), ' '), '') FROM audit_log WHERE event LIKE 'channel_%'`).Scan(&details); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"TOKEN", "hmac-", "otro-hmac", "https://", "/services", "ULTRA"} {
		if strings.Contains(details, secret) {
			t.Errorf("la auditoria filtra %q: %s", secret, details)
		}
	}
	if !strings.Contains(details, "hooks.example.com") || !strings.Contains(details, "Slack ops") || !strings.Contains(details, "webhook") {
		t.Errorf("la auditoria debe llevar nombre, tipo y host: %s", details)
	}
	if !strings.Contains(details, `"url", "hmac_secret", "enabled"`) {
		t.Errorf("el update debe auditar los NOMBRES de los campos cambiados: %s", details)
	}
}

// Flujo completo: regla -> alerta -> entrega en la cola -> worker real -> envio con el secreto descifrado.
func TestAlertOpensAndResolvesThroughTheWorkerToTheChannel(t *testing.T) {
	e := newChannelEnv(t)
	id := e.createChannel(chBody("Slack ops"))
	ruleID := e.createRule(cpuRuleBody)
	e.wantStatus(e.do("PUT", "/api/v1/alert-rules/"+ruleID+"/channels", fmt.Sprintf(`{"channel_ids":[%q]}`, id), withCookie(e.admin)), http.StatusOK, "")
	hostID := e.host("srv-web-01", 97)
	e.hook.sent, e.hook.bodies = nil, nil // descarta lo de las pruebas

	w := notify.NewWorker(e.st, e.box, e.hook, slog.New(slog.NewTextHandler(io.Discard, nil)), "https://argos.example.com")
	run := func() notify.Stats {
		st, err := w.RunOnce(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return st
	}

	e.eval() // abre la alerta y encola 'opened'
	if st := run(); st.Sent != 1 || len(e.hook.sent) != 1 {
		t.Fatalf("worker tras abrir got %+v, envios %d, want 1", st, len(e.hook.sent))
	}
	got := e.hook.sent[0]
	if got.URL != hookURL || got.HMACSecret != hookHMAC {
		t.Errorf("el worker debe descifrar y usar el secreto del canal: %+v", got)
	}
	body := string(e.hook.bodies[0])
	for _, want := range []string{`"hostname":"srv-web-01"`, `"rule":"CPU alta"`, `"severity":"critical"`, `"state":"firing"`, `"threshold":90`, `"url":"https://argos.example.com/alertas/`} {
		if !strings.Contains(body, want) {
			t.Errorf("el mensaje no contiene %s: %s", want, body)
		}
	}
	for _, leak := range []string{"192.168", "10.0.0", "label", hostID, hookHMAC, "TOKEN"} {
		if strings.Contains(body, leak) {
			t.Errorf("el mensaje contiene %q (solo hostname, regla, severidad, valor, umbral y enlace): %s", leak, body)
		}
	}
	if n := e.count(`SELECT count(*) FROM notification_deliveries WHERE status = 'sent' AND event = 'opened'`); n != 1 {
		t.Errorf("entrega opened enviada got %d, want 1", n)
	}
	if st := run(); st.Claimed != 0 {
		t.Errorf("una entrega enviada no se reenvia: %+v", st)
	}

	// Resolver: el host se recupera.
	mustExec(t, `UPDATE host_latest SET cpu_pct = 10, ts = now() WHERE host_id = $1`, hostID)
	e.eval()
	if st := run(); st.Sent != 1 || len(e.hook.sent) != 2 || !strings.Contains(string(e.hook.bodies[1]), `"state":"resolved"`) {
		t.Errorf("aviso de resolucion: %+v, envios %d", st, len(e.hook.sent))
	}
}

func TestFailedSendIsRetriedUntilItDies(t *testing.T) {
	e := newChannelEnv(t)
	id := e.createChannel(chBody("Slack ops"))
	ruleID := e.createRule(cpuRuleBody)
	e.wantStatus(e.do("PUT", "/api/v1/alert-rules/"+ruleID+"/channels", fmt.Sprintf(`{"channel_ids":[%q]}`, id), withCookie(e.admin)), http.StatusOK, "")
	e.host("srv", 97)
	e.eval()
	e.hook.sendErr, e.hook.code = &notify.SendError{Category: notify.CatStatus, HTTPStatus: 500}, 500
	w := notify.NewWorker(e.st, e.box, e.hook, slog.New(slog.NewTextHandler(io.Discard, nil)), "")

	for attempt := 1; attempt <= store.MaxDeliveryAttempts; attempt++ {
		mustExec(t, `UPDATE notification_deliveries SET next_attempt_at = now() WHERE status = 'pending'`)
		if st, err := w.RunOnce(context.Background()); err != nil || st.Failed != 1 {
			t.Fatalf("intento %d: %+v, %v", attempt, st, err)
		}
	}
	var status, lastErr string
	var attempts, httpStatus int
	if err := testPool.QueryRow(context.Background(),
		`SELECT status, attempts, last_error, http_status FROM notification_deliveries`).Scan(&status, &attempts, &lastErr, &httpStatus); err != nil {
		t.Fatal(err)
	}
	if status != "dead" || attempts != store.MaxDeliveryAttempts || lastErr != "respuesta http 500" || httpStatus != 500 {
		t.Errorf("got %s attempts=%d err=%q http=%d, want dead tras %d intentos", status, attempts, lastErr, httpStatus, store.MaxDeliveryAttempts)
	}
	if strings.Contains(lastErr, "hooks.example.com") {
		t.Error("last_error no debe llevar la URL")
	}
	mustExec(t, `UPDATE notification_deliveries SET next_attempt_at = now()`)
	if st, _ := w.RunOnce(context.Background()); st.Claimed != 0 {
		t.Error("una entrega dead no se reintenta")
	}
}

func TestChannelRoutesAreRegisteredWithTheExpectedAccess(t *testing.T) {
	e := newEnv(t, Options{})
	want := map[string]access{
		"GET /api/v1/channels": accessSession, "GET /api/v1/channels/{id}": accessSession,
		"POST /api/v1/channels": accessAdmin, "PATCH /api/v1/channels/{id}": accessAdmin,
		"DELETE /api/v1/channels/{id}": accessAdmin, "POST /api/v1/channels/{id}/test": accessAdmin,
		"PUT /api/v1/alert-rules/{id}/channels": accessAdmin,
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

func TestSecretKeyFixtureIsA32ByteBase64Key(t *testing.T) {
	raw, err := base64.StdEncoding.DecodeString(channelKey)
	if err != nil || len(raw) != 32 {
		t.Fatalf("channelKey de test invalida: %d bytes, %v", len(raw), err)
	}
}

// Arranque: con canales cifrados en la BD real, el server exige la misma clave (si no, no arranca).
func TestStartupRefusesAMissingOrDifferentKeyWhenChannelsExist(t *testing.T) {
	e := newChannelEnv(t)
	ctx := context.Background()
	if err := notify.CheckSecretKey(ctx, e.st, nil); err != nil {
		t.Fatalf("sin canales no se exige la clave: %v", err)
	}
	e.createChannel(chBody("Slack ops"))
	if err := notify.CheckSecretKey(ctx, e.st, e.box); err != nil {
		t.Errorf("con la clave correcta debe arrancar: %v", err)
	}
	if err := notify.CheckSecretKey(ctx, e.st, nil); err == nil || !strings.Contains(err.Error(), "ARGOS_SECRET_KEY") {
		t.Errorf("sin clave y con canales debe fallar diciendo que falta: %v", err)
	}
	other, err := notify.NewBox(base64.StdEncoding.EncodeToString([]byte("otra-clave-de-32-bytes-exactos!!")))
	if err != nil {
		t.Fatal(err)
	}
	if err := notify.CheckSecretKey(ctx, e.st, other); err == nil || !strings.Contains(err.Error(), "no coincide") {
		t.Errorf("con otra clave debe fallar diciendo que no coincide: %v", err)
	}
}
