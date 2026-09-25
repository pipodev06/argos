package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"argos/server/internal/store"
)

// Tests de seguridad contra una Postgres real (ver main_test.go). No usan t.Parallel:
// comparten BD y cada uno la deja vacia al empezar.

const (
	pwAdmin  = "correct-horse-battery-1"
	pwViewer = "viewer-strong-password-2"
	anyUUID  = "00000000-0000-4000-8000-000000000000"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type env struct {
	t     *testing.T
	api   *API
	h     http.Handler
	st    *store.Store
	clock *fakeClock
	logs  *logBuffer
}

// newEnv deja la BD vacia y arma la API real (store real, reloj controlable).
func newEnv(t *testing.T, opts Options) *env {
	t.Helper()
	if testPool == nil {
		t.Skip("ARGOS_TEST_DATABASE_URL no definida")
	}
	if _, err := testPool.Exec(context.Background(),
		`TRUNCATE service_check_defs, service_check_events, service_checks, metrics, metrics_hourly, maintenance_state, disk_samples, enroll_tokens, hosts, host_groups, alert_rules, notification_channels, maintenance_windows, sessions, audit_log, users CASCADE`); err != nil {
		t.Fatalf("limpiando tablas: %v", err)
	}
	logs := &logBuffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	clock := &fakeClock{t: time.Now()}
	st := store.New(testPool)
	api := New(st, testHasher(), logger, clock.Now, opts)
	return &env{t: t, api: api, h: api.Routes(), st: st, clock: clock, logs: logs}
}

type reqOpt func(*http.Request)

func withCookie(c *http.Cookie) reqOpt { return func(r *http.Request) { r.AddCookie(c) } }
func withHeader(k, v string) reqOpt    { return func(r *http.Request) { r.Header.Set(k, v) } }
func withHost(h string) reqOpt         { return func(r *http.Request) { r.Host = h } }
func fromAddr(a string) reqOpt         { return func(r *http.Request) { r.RemoteAddr = a } }

func (e *env) do(method, path, body string, opts ...reqOpt) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "localhost:8080"
	req.RemoteAddr = "192.0.2.10:4321"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, o := range opts {
		o(req)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func (e *env) newUser(username, password, role string, mustChange bool) store.User {
	e.t.Helper()
	hash, err := e.api.hasher.Hash(context.Background(), password)
	if err != nil {
		e.t.Fatal(err)
	}
	u, err := e.st.CreateUser(context.Background(), username, hash, role, mustChange)
	if err != nil {
		e.t.Fatalf("creando usuario %q: %v", username, err)
	}
	return u
}

func (e *env) sessionCookieOf(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == e.api.cookieName && c.Value != "" {
			return c
		}
	}
	return nil
}

func loginBody(user, pass string) string {
	b, _ := json.Marshal(map[string]string{"username": user, "password": pass})
	return string(b)
}

// login inicia sesion y devuelve la cookie (falla el test si no hay).
func (e *env) login(user, pass string, opts ...reqOpt) *http.Cookie {
	e.t.Helper()
	rec := e.do("POST", "/api/v1/auth/login", loginBody(user, pass), opts...)
	if rec.Code != http.StatusOK {
		e.t.Fatalf("login de %q: status %d, body %s", user, rec.Code, rec.Body.String())
	}
	c := e.sessionCookieOf(rec)
	if c == nil {
		e.t.Fatalf("login de %q no emitio cookie de sesion", user)
	}
	return c
}

func (e *env) wantStatus(rec *httptest.ResponseRecorder, status int, code string) {
	e.t.Helper()
	if rec.Code != status {
		e.t.Fatalf("status got %d, want %d (body %s)", rec.Code, status, rec.Body.String())
	}
	if code == "" {
		return
	}
	var resp response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Error == nil || resp.Error.Code != code {
		e.t.Fatalf("error code got %s, want %q", rec.Body.String(), code)
	}
}

func (e *env) count(sql string, args ...any) int {
	e.t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		e.t.Fatalf("query %q: %v", sql, err)
	}
	return n
}

func (e *env) dataAs(rec *httptest.ResponseRecorder, dst any) {
	e.t.Helper()
	var resp response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		e.t.Fatalf("respuesta no es envelope: %v", err)
	}
	if err := json.Unmarshal(resp.Data, dst); err != nil {
		e.t.Fatalf("data no coincide con %T: %v (%s)", dst, err, resp.Data)
	}
}

// adminCookie crea un admin y devuelve su cookie de sesion.
func (e *env) adminCookie() *http.Cookie {
	e.t.Helper()
	e.newUser("admin", pwAdmin, store.RoleAdmin, false)
	return e.login("admin", pwAdmin)
}

// enrollHost crea un enroll token como admin y lo canjea; devuelve host id y agent token.
func (e *env) enrollHost(admin *http.Cookie, hostname string, opts ...reqOpt) (hostID, agentToken, enrollToken string) {
	e.t.Helper()
	rec := e.do("POST", "/api/v1/hosts/enroll-token", `{"hostname":"`+hostname+`"}`, withCookie(admin))
	e.wantStatus(rec, http.StatusCreated, "")
	var tok struct {
		EnrollToken string `json:"enroll_token"`
	}
	e.dataAs(rec, &tok)

	rec = e.do("POST", "/api/v1/agent/enroll", `{"enroll_token":"`+tok.EnrollToken+`","os":"linux","arch":"amd64"}`, opts...)
	e.wantStatus(rec, http.StatusCreated, "")
	var enr struct {
		HostID     string `json:"host_id"`
		AgentToken string `json:"agent_token"`
	}
	e.dataAs(rec, &enr)
	return enr.HostID, enr.AgentToken, tok.EnrollToken
}

func (e *env) metrics(agentToken string, opts ...reqOpt) *httptest.ResponseRecorder {
	opts = append(opts, withHeader("Authorization", "Bearer "+agentToken))
	return e.do("POST", "/api/v1/agent/metrics", `{"metrics":{"cpu_pct":1}}`, opts...)
}

// --- deny by default ---

func splitPattern(pattern string) (method, path string) {
	method, path, _ = strings.Cut(pattern, " ")
	return method, strings.ReplaceAll(strings.ReplaceAll(path, "{id}", anyUUID), "{check_id}", anyUUID)
}

func TestEveryDashboardRouteRequiresSession(t *testing.T) {
	e := newEnv(t, Options{})

	var dashboard int
	for _, rt := range e.api.routes {
		if rt.access < accessSessionAny {
			continue
		}
		dashboard++
		method, path := splitPattern(rt.pattern)
		t.Run(rt.pattern, func(t *testing.T) {
			e.wantStatus(e.do(method, path, ""), http.StatusUnauthorized, "unauthenticated")
			bogus := &http.Cookie{Name: e.api.cookieName, Value: "cookie-que-no-existe"}
			e.wantStatus(e.do(method, path, "", withCookie(bogus)), http.StatusUnauthorized, "unauthenticated")
		})
	}
	if dashboard < 9 {
		t.Errorf("rutas de dashboard registradas got %d, want >= 9", dashboard)
	}
}

// Fija el conjunto de rutas sin sesion: una ruta nueva registrada como publica por
// error rompe este test y obliga a decidirlo a proposito.
func TestPublicAndAgentRoutesAreExactlyTheExpectedOnes(t *testing.T) {
	e := newEnv(t, Options{})
	var public, agent []string
	for _, rt := range e.api.routes {
		switch rt.access {
		case accessPublic:
			public = append(public, rt.pattern)
		case accessAgent:
			agent = append(agent, rt.pattern)
		}
	}
	slices.Sort(public)
	// Instaladores y descargas del agente: publicas a proposito (spec fase 3), con limite de tasa por IP.
	want := []string{
		"GET /dl/latest/manifest.json", "GET /dl/{version}/{file}", "GET /health", "GET /install.ps1", "GET /install.sh",
		"POST /api/v1/agent/enroll", "POST /api/v1/auth/login",
	}
	if !slices.Equal(public, want) {
		t.Errorf("rutas publicas got %v, want %v", public, want)
	}
	if want := []string{"POST /api/v1/agent/metrics", "POST /api/v1/agent/inventory"}; !slices.Equal(agent, want) {
		t.Errorf("rutas de agente got %v, want %v", agent, want)
	}
}

func TestAgentRoutesIgnoreSessionCookie(t *testing.T) {
	e := newEnv(t, Options{})
	admin := e.adminCookie()
	// Una cookie de sesion valida no autentica a un agente: solo vale el bearer.
	rec := e.do("POST", "/api/v1/agent/metrics", `{}`, withCookie(admin))
	e.wantStatus(rec, http.StatusUnauthorized, "invalid_token")
}

func TestViewerForbiddenOnAdminRoutes(t *testing.T) {
	e := newEnv(t, Options{})
	e.newUser("viewer", pwViewer, store.RoleViewer, false)
	viewer := e.login("viewer", pwViewer)

	n := 0
	for _, rt := range e.api.routes {
		if rt.access != accessAdmin {
			continue
		}
		n++
		method, path := splitPattern(rt.pattern)
		t.Run(rt.pattern, func(t *testing.T) {
			e.wantStatus(e.do(method, path, "", withCookie(viewer)), http.StatusForbidden, "forbidden")
		})
	}
	if got := e.count(`SELECT count(*) FROM audit_log WHERE event = 'authz_denied' AND outcome = 'denied'`); got != n {
		t.Errorf("authz_denied auditados got %d, want %d", got, n)
	}
	// GET /hosts es de lectura: el viewer si puede.
	e.wantStatus(e.do("GET", "/api/v1/hosts", "", withCookie(viewer)), http.StatusOK, "")
}

// --- CSRF, Content-Type, Host ---

func TestCrossSiteRequestsAreRejected(t *testing.T) {
	e := newEnv(t, Options{})
	admin := e.adminCookie()
	const body = `{"hostname":"srv"}`
	tests := []struct {
		name       string
		hdr        map[string]string
		wantStatus int
	}{
		{"cross-site", map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
		{"same-site (otro subdominio)", map[string]string{"Sec-Fetch-Site": "same-site"}, 403},
		{"origin ajeno sin sec-fetch", map[string]string{"Origin": "https://evil.example"}, 403},
		{"same-origin", map[string]string{"Sec-Fetch-Site": "same-origin"}, 201},
		{"origin propio", map[string]string{"Origin": "http://localhost:8080"}, 201},
		{"sin cabeceras de navegador (curl)", nil, 201},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := []reqOpt{withCookie(admin)}
			for k, v := range tt.hdr {
				opts = append(opts, withHeader(k, v))
			}
			rec := e.do("POST", "/api/v1/hosts/enroll-token", body, opts...)
			if tt.wantStatus == 403 {
				e.wantStatus(rec, 403, "cross_origin")
			} else {
				e.wantStatus(rec, tt.wantStatus, "")
			}
		})
	}
	// El login tambien es una escritura: un POST cross-site tampoco pasa.
	rec := e.do("POST", "/api/v1/auth/login", loginBody("admin", pwAdmin), withHeader("Sec-Fetch-Site", "cross-site"))
	e.wantStatus(rec, 403, "cross_origin")
}

func TestJSONContentTypeIsRequiredForBodies(t *testing.T) {
	e := newEnv(t, Options{})
	e.newUser("admin", pwAdmin, store.RoleAdmin, false)
	body := loginBody("admin", pwAdmin)

	for _, ct := range []string{"text/plain", "application/x-www-form-urlencoded", "multipart/form-data", "application/jsonx", ""} {
		rec := e.do("POST", "/api/v1/auth/login", body, withHeader("Content-Type", ct))
		e.wantStatus(rec, http.StatusUnsupportedMediaType, "unsupported_media_type")
	}
	for _, ct := range []string{"application/json", "application/json; charset=utf-8", "Application/JSON"} {
		rec := e.do("POST", "/api/v1/auth/login", body, withHeader("Content-Type", ct))
		e.wantStatus(rec, http.StatusOK, "")
	}
	// Sin body no hay Content-Type que exigir (logout, revoke): los protegen SameSite y CrossOriginProtection.
	c := e.login("admin", pwAdmin)
	e.wantStatus(e.do("POST", "/api/v1/auth/logout", "", withCookie(c)), http.StatusOK, "")
	// Los agentes tambien deben mandar JSON.
	e.wantStatus(e.do("POST", "/api/v1/agent/enroll", `{}`, withHeader("Content-Type", "text/plain")), 415, "unsupported_media_type")
}

func TestHostAllowlist(t *testing.T) {
	pub, _ := url.Parse("https://argos.example.com")
	tests := []struct {
		name       string
		opts       Options
		host, path string
		wantStatus int
	}{
		{"localhost con puerto", Options{}, "localhost:8080", "/api/v1/hosts", 401},
		{"vite dev", Options{}, "localhost:5173", "/api/v1/hosts", 401},
		{"127.0.0.1", Options{}, "127.0.0.1:8080", "/api/v1/hosts", 401},
		{"ipv6 loopback", Options{}, "[::1]:8080", "/api/v1/hosts", 401},
		{"host falso", Options{}, "evil.example.com", "/api/v1/hosts", 421},
		{"rebinding con puerto", Options{}, "evil.example.com:8080", "/api/v1/auth/me", 421},
		{"publico sin configurar", Options{}, "argos.example.com", "/api/v1/hosts", 421},
		{"publico configurado", Options{PublicURL: pub}, "argos.example.com", "/api/v1/hosts", 401},
		{"publico en mayusculas", Options{PublicURL: pub}, "ARGOS.example.com:443", "/api/v1/hosts", 401},
		{"login con host falso", Options{}, "evil.example.com", "/api/v1/auth/login", 421},
		{"agente por IP no se filtra", Options{}, "10.1.2.3:8080", "/api/v1/agent/metrics", 401},
		{"health por IP", Options{}, "10.1.2.3:8080", "/health", 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, tt.opts)
			method := "GET"
			if strings.HasSuffix(tt.path, "login") || strings.HasSuffix(tt.path, "metrics") {
				method = "POST"
			}
			rec := e.do(method, tt.path, "", withHost(tt.host))
			if rec.Code != tt.wantStatus {
				t.Errorf("status got %d, want %d (%s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestSecurityHeaders(t *testing.T) {
	httpsURL, _ := url.Parse("https://argos.example.com")
	httpURL, _ := url.Parse("http://argos.example.com")

	e := newEnv(t, Options{})
	rec := e.do("GET", "/api/v1/auth/me", "")
	want := map[string]string{
		"Cache-Control":           "no-store",
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "no-referrer",
		"Content-Security-Policy": "default-src 'self'; frame-ancestors 'none'",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s got %q, want %q", k, got, v)
		}
	}
	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS sin https configurado got %q, want vacio", got)
	}
	if got := e.do("GET", "/health", "").Header().Get("Cache-Control"); got != "" {
		t.Errorf("Cache-Control fuera de /api got %q, want vacio", got)
	}
	// Las respuestas de error tambien llevan las cabeceras (421 del filtro de Host).
	if got := e.do("GET", "/api/v1/hosts", "", withHost("evil.example")).Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("nosniff en 421 got %q", got)
	}

	if got := newEnv(t, Options{PublicURL: httpsURL}).do("GET", "/health", "", withHost("argos.example.com")).Header().Get("Strict-Transport-Security"); got == "" {
		t.Error("con ARGOS_PUBLIC_URL https debe haber HSTS")
	}
	if got := newEnv(t, Options{PublicURL: httpURL}).do("GET", "/health", "", withHost("argos.example.com")).Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("con ARGOS_PUBLIC_URL http no debe haber HSTS, got %q", got)
	}
}

// --- login y sesion ---

func TestLoginSetsHardenedCookieAndStoresOnlyHash(t *testing.T) {
	e := newEnv(t, Options{})
	e.newUser("alice", pwViewer, store.RoleViewer, false)

	rec := e.do("POST", "/api/v1/auth/login", loginBody("alice", pwViewer), withHeader("User-Agent", strings.Repeat("ñ", 500)))
	e.wantStatus(rec, http.StatusOK, "")

	raw := rec.Header().Get("Set-Cookie")
	for _, want := range []string{"__Host-argos_sid=", "Path=/", "HttpOnly", "Secure", "SameSite=Strict"} {
		if !strings.Contains(raw, want) {
			t.Errorf("Set-Cookie %q no contiene %q", raw, want)
		}
	}
	for _, bad := range []string{"Domain", "Max-Age", "Expires"} {
		if strings.Contains(raw, bad) {
			t.Errorf("Set-Cookie %q no debe contener %q", raw, bad)
		}
	}
	c := e.sessionCookieOf(rec)
	if len(c.Value) != 43 {
		t.Errorf("largo del ID de sesion got %d, want 43 (32 bytes en base64url)", len(c.Value))
	}

	sum := sha256.Sum256([]byte(c.Value))
	if got := e.count(`SELECT count(*) FROM sessions WHERE id_hash = $1`, sum[:]); got != 1 {
		t.Errorf("la BD no tiene la sesion indexada por el SHA-256 del ID")
	}
	if got := e.count(`SELECT count(*) FROM sessions s WHERE s::text LIKE '%' || $1 || '%'`, c.Value); got != 0 {
		t.Errorf("el ID de sesion aparece en claro en la BD")
	}
	if got := e.count(`SELECT count(*) FROM sessions WHERE char_length(user_agent) = 200`); got != 1 {
		t.Errorf("el user_agent no se trunco a 200 caracteres")
	}

	var body struct {
		User userDTO `json:"user"`
	}
	e.dataAs(rec, &body)
	if body.User.Username != "alice" || body.User.Role != "viewer" || body.User.MustChangePassword {
		t.Errorf("user del login inesperado: %+v", body.User)
	}
}

func TestSessionIDChangesOnEveryLogin(t *testing.T) {
	e := newEnv(t, Options{})
	e.newUser("alice", pwViewer, store.RoleViewer, false)

	first := e.login("alice", pwViewer)
	// Aun presentando la cookie anterior, el login emite un ID nuevo (no hay session fixation).
	second := e.login("alice", pwViewer, withCookie(first))
	if first.Value == second.Value {
		t.Fatal("el ID de sesion no cambio tras un nuevo login")
	}
	// Un ID inventado por el atacante nunca se adopta.
	planted := &http.Cookie{Name: e.api.cookieName, Value: "id-plantado-por-el-atacante"}
	third := e.login("alice", pwViewer, withCookie(planted))
	if third.Value == planted.Value {
		t.Fatal("el login adopto un ID de sesion enviado por el cliente")
	}
	e.wantStatus(e.do("GET", "/api/v1/auth/me", "", withCookie(planted)), http.StatusUnauthorized, "unauthenticated")
}

func TestLogoutRevokesSessionAndExpiresCookie(t *testing.T) {
	e := newEnv(t, Options{})
	e.newUser("alice", pwViewer, store.RoleViewer, false)
	c := e.login("alice", pwViewer)

	rec := e.do("POST", "/api/v1/auth/logout", "", withCookie(c))
	e.wantStatus(rec, http.StatusOK, "")
	if got := rec.Header().Get("Set-Cookie"); !strings.Contains(got, "Max-Age=0") || !strings.Contains(got, e.api.cookieName+"=;") {
		t.Errorf("logout debe expirar la cookie, Set-Cookie got %q", got)
	}
	e.wantStatus(e.do("GET", "/api/v1/auth/me", "", withCookie(c)), http.StatusUnauthorized, "unauthenticated")
	if got := e.count(`SELECT count(*) FROM sessions`); got != 0 {
		t.Errorf("sesiones tras logout got %d, want 0", got)
	}
	if got := e.count(`SELECT count(*) FROM audit_log WHERE event = 'logout' AND outcome = 'ok'`); got != 1 {
		t.Errorf("logout auditado got %d, want 1", got)
	}
}

func TestSessionExpiresByInactivity(t *testing.T) {
	e := newEnv(t, Options{})
	e.newUser("alice", pwViewer, store.RoleViewer, false)
	c := e.login("alice", pwViewer)

	// La actividad renueva la ventana de inactividad.
	for range 3 {
		e.clock.Advance(29 * time.Minute)
		e.wantStatus(e.do("GET", "/api/v1/auth/me", "", withCookie(c)), http.StatusOK, "")
	}
	e.clock.Advance(31 * time.Minute)
	rec := e.do("GET", "/api/v1/auth/me", "", withCookie(c))
	e.wantStatus(rec, http.StatusUnauthorized, "unauthenticated")
	if got := rec.Header().Get("Set-Cookie"); !strings.Contains(got, "Max-Age=0") {
		t.Errorf("una sesion vencida debe expirar la cookie, Set-Cookie got %q", got)
	}
	if got := e.count(`SELECT count(*) FROM sessions`); got != 0 {
		t.Errorf("la sesion vencida debe borrarse, quedan %d", got)
	}
}

func TestSessionExpiresAtAbsoluteLimit(t *testing.T) {
	e := newEnv(t, Options{})
	e.newUser("alice", pwViewer, store.RoleViewer, false)
	c := e.login("alice", pwViewer)

	// Sesion siempre activa (cada 20 min) durante 7 h 40 min: sigue viva.
	for range 23 {
		e.clock.Advance(20 * time.Minute)
		e.wantStatus(e.do("GET", "/api/v1/auth/me", "", withCookie(c)), http.StatusOK, "")
	}
	// A las 8 h 01 min, aunque este activa, vence por el limite absoluto.
	e.clock.Advance(21 * time.Minute)
	e.wantStatus(e.do("GET", "/api/v1/auth/me", "", withCookie(c)), http.StatusUnauthorized, "unauthenticated")
}

func TestLastSeenIsTouchedAtMostOncePerMinute(t *testing.T) {
	e := newEnv(t, Options{})
	e.newUser("alice", pwViewer, store.RoleViewer, false)
	c := e.login("alice", pwViewer)
	created := e.clock.Now()

	lastSeen := func() time.Time {
		var ts time.Time
		if err := testPool.QueryRow(context.Background(), `SELECT last_seen_at FROM sessions`).Scan(&ts); err != nil {
			t.Fatal(err)
		}
		return ts
	}
	near := func(a, b time.Time) bool { return a.Sub(b).Abs() < time.Millisecond }

	e.clock.Advance(30 * time.Second)
	e.wantStatus(e.do("GET", "/api/v1/auth/me", "", withCookie(c)), http.StatusOK, "")
	if got := lastSeen(); !near(got, created) {
		t.Errorf("a los 30 s last_seen_at cambio: %v -> %v", created, got)
	}
	e.clock.Advance(40 * time.Second)
	e.wantStatus(e.do("GET", "/api/v1/auth/me", "", withCookie(c)), http.StatusOK, "")
	if got := lastSeen(); !near(got, e.clock.Now()) {
		t.Errorf("a los 70 s last_seen_at got %v, want %v", got, e.clock.Now())
	}
}

func TestDisablingAUserKillsHisSessionImmediately(t *testing.T) {
	e := newEnv(t, Options{})
	admin := e.adminCookie()
	viewer := e.newUser("viewer", pwViewer, store.RoleViewer, false)
	vc := e.login("viewer", pwViewer)

	rec := e.do("PATCH", "/api/v1/users/"+viewer.ID, `{"disabled":true}`, withCookie(admin))
	e.wantStatus(rec, http.StatusOK, "")
	e.wantStatus(e.do("GET", "/api/v1/hosts", "", withCookie(vc)), http.StatusUnauthorized, "unauthenticated")
	e.wantStatus(e.do("POST", "/api/v1/auth/login", loginBody("viewer", pwViewer)), http.StatusUnauthorized, "invalid_credentials")
}

func TestSessionStoreFailureIs500NotUnauthorized(t *testing.T) {
	t.Parallel()
	fs := &fakeStore{sessionErr: errors.New("db caida")}
	h, logs := newTestAPI(t, fs)
	rec, resp := do(t, h, "GET", "/api/v1/hosts", "", "", asSession)
	wantError(t, rec, resp, 500, "internal_error")
	if !strings.Contains(logs.String(), "db caida") || strings.Contains(rec.Body.String(), "db caida") {
		t.Errorf("el 500 debe loguear el error real y no filtrarlo al cliente")
	}
}

// --- credenciales, bloqueo y limites ---

func TestFailedLoginsAreIndistinguishable(t *testing.T) {
	e := newEnv(t, Options{})
	e.newUser("alice", pwViewer, store.RoleViewer, false)
	bob := e.newUser("bob", pwViewer, store.RoleViewer, false)
	carol := e.newUser("carol", pwViewer, store.RoleViewer, false)
	mustExec(t, `UPDATE users SET disabled_at = now() WHERE id = $1`, bob.ID)
	mustExec(t, `UPDATE users SET locked_until = $2 WHERE id = $1`, carol.ID, e.clock.Now().Add(time.Hour))

	attempts := []struct{ name, user, pass string }{
		{"usuario inexistente", "ghost", pwViewer},
		{"contraseña incorrecta", "alice", "incorrecta-incorrecta-1"},
		{"cuenta deshabilitada, clave correcta", "bob", pwViewer},
		{"cuenta bloqueada, clave correcta", "carol", pwViewer},
	}
	var first *httptest.ResponseRecorder
	for _, at := range attempts {
		rec := e.do("POST", "/api/v1/auth/login", loginBody(at.user, at.pass))
		e.wantStatus(rec, http.StatusUnauthorized, "invalid_credentials")
		if rec.Header().Get("Set-Cookie") != "" {
			t.Errorf("%s: un login fallido no debe emitir cookie", at.name)
		}
		if first == nil {
			first = rec
			continue
		}
		if !bytes.Equal(rec.Body.Bytes(), first.Body.Bytes()) {
			t.Errorf("%s: cuerpo distinto\n got  %s\n want %s", at.name, rec.Body.String(), first.Body.String())
		}
	}
}

func TestLoginOfUnknownUserStillPaysTheHashCost(t *testing.T) {
	e := newEnv(t, Options{})
	start := time.Now()
	e.do("POST", "/api/v1/auth/login", loginBody("ghost", pwViewer))
	// Sin el hash "dummy" esta respuesta tardaria ~1 ms (solo una consulta); un Argon2id real, decenas.
	if d := time.Since(start); d < 5*time.Millisecond {
		t.Errorf("login de usuario inexistente tardo %v: no se esta igualando el costo del hash", d)
	}
}

func TestAccountLockoutAfterTenFailuresAndCLIResetStillWorks(t *testing.T) {
	e := newEnv(t, Options{})
	alice := e.newUser("alice", pwViewer, store.RoleViewer, false)

	for i := 1; i <= 10; i++ {
		rec := e.do("POST", "/api/v1/auth/login", loginBody("alice", "mala-contraseña-numero-"+strconv.Itoa(i)))
		e.wantStatus(rec, http.StatusUnauthorized, "invalid_credentials")
	}
	// Bloqueada: ni la contraseña correcta entra, y responde igual que un fallo cualquiera.
	e.wantStatus(e.do("POST", "/api/v1/auth/login", loginBody("alice", pwViewer)), http.StatusUnauthorized, "invalid_credentials")
	u, err := e.st.UserByID(context.Background(), alice.ID)
	if err != nil || u.LockedUntil == nil || !u.LockedUntil.After(e.clock.Now()) {
		t.Fatalf("la cuenta debio quedar bloqueada: %+v (%v)", u, err)
	}
	if got := e.count(`SELECT count(*) FROM audit_log WHERE event = 'login_fail' AND detail->>'locked' = 'true'`); got != 1 {
		t.Errorf("el fallo que bloqueo la cuenta se audita con locked=true, got %d", got)
	}

	// reset-password (via CLI) desbloquea sin esperar los 15 min.
	newHash, err := e.api.hasher.Hash(context.Background(), "contraseña-nueva-del-cli-9")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.ResetPasswordByUsername(context.Background(), "alice", newHash, e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	e.login("alice", "contraseña-nueva-del-cli-9")
}

func TestLockExpiresAfterFifteenMinutes(t *testing.T) {
	e := newEnv(t, Options{})
	e.newUser("alice", pwViewer, store.RoleViewer, false)
	for range 10 {
		e.do("POST", "/api/v1/auth/login", loginBody("alice", "mala-contraseña-larga-0"))
	}
	e.wantStatus(e.do("POST", "/api/v1/auth/login", loginBody("alice", pwViewer)), http.StatusUnauthorized, "invalid_credentials")
	e.clock.Advance(14 * time.Minute)
	e.wantStatus(e.do("POST", "/api/v1/auth/login", loginBody("alice", pwViewer)), http.StatusUnauthorized, "invalid_credentials")
	e.clock.Advance(2 * time.Minute)
	e.login("alice", pwViewer)
}

func TestSuccessfulLoginResetsFailureCounter(t *testing.T) {
	e := newEnv(t, Options{})
	alice := e.newUser("alice", pwViewer, store.RoleViewer, false)
	for range 9 {
		e.do("POST", "/api/v1/auth/login", loginBody("alice", "mala-contraseña-larga-0"))
	}
	e.login("alice", pwViewer)
	if u, _ := e.st.UserByID(context.Background(), alice.ID); u.FailedLogins != 0 {
		t.Errorf("failed_logins tras un login exitoso got %d, want 0", u.FailedLogins)
	}
	// 9 fallos mas: no alcanzan los 10 consecutivos.
	for range 9 {
		e.do("POST", "/api/v1/auth/login", loginBody("alice", "mala-contraseña-larga-0"))
	}
	e.login("alice", pwViewer)
}

func TestLoginRateLimitPerIP(t *testing.T) {
	e := newEnv(t, Options{})
	const ip = "198.51.100.7:5555"
	// Body invalido: gasta un token del limitador sin pagar el hash.
	for i := 1; i <= 20; i++ {
		e.wantStatus(e.do("POST", "/api/v1/auth/login", `{`, fromAddr(ip)), http.StatusBadRequest, "invalid_body")
	}
	rec := e.do("POST", "/api/v1/auth/login", `{`, fromAddr(ip))
	e.wantStatus(rec, http.StatusTooManyRequests, "rate_limited")
	if secs, err := strconv.Atoi(rec.Header().Get("Retry-After")); err != nil || secs < 1 {
		t.Errorf("Retry-After got %q, want segundos >= 1", rec.Header().Get("Retry-After"))
	}
	// Falsificar X-Forwarded-For desde un cliente que no es proxy de confianza no evade el limite.
	rec = e.do("POST", "/api/v1/auth/login", `{`, fromAddr(ip), withHeader("X-Forwarded-For", "203.0.113.99"))
	e.wantStatus(rec, http.StatusTooManyRequests, "rate_limited")
	// Otra IP no se ve afectada.
	e.wantStatus(e.do("POST", "/api/v1/auth/login", `{`, fromAddr("198.51.100.8:1")), http.StatusBadRequest, "invalid_body")
	// El bucket se recupera con el tiempo (1 token cada 30 s).
	e.clock.Advance(31 * time.Second)
	e.wantStatus(e.do("POST", "/api/v1/auth/login", `{`, fromAddr(ip)), http.StatusBadRequest, "invalid_body")
}

func TestLoginRateLimitUsesForwardedForOnlyBehindTrustedProxy(t *testing.T) {
	e := newEnv(t, Options{TrustedProxies: mustPrefixes(t, "10.0.0.0/8")})
	proxy := fromAddr("10.0.0.1:443")
	// Cada cliente detras del proxy tiene su bucket.
	for i := 1; i <= 20; i++ {
		e.wantStatus(e.do("POST", "/api/v1/auth/login", `{`, proxy, withHeader("X-Forwarded-For", "203.0.113.1")), 400, "invalid_body")
	}
	e.wantStatus(e.do("POST", "/api/v1/auth/login", `{`, proxy, withHeader("X-Forwarded-For", "203.0.113.1")), 429, "rate_limited")
	e.wantStatus(e.do("POST", "/api/v1/auth/login", `{`, proxy, withHeader("X-Forwarded-For", "203.0.113.2")), 400, "invalid_body")
	// Un valor falso a la izquierda de la IP real que anadio el proxy no sirve.
	e.wantStatus(e.do("POST", "/api/v1/auth/login", `{`, proxy, withHeader("X-Forwarded-For", "1.2.3.4, 203.0.113.1")), 429, "rate_limited")
}

// --- cambio de contraseña ---

func TestChangePassword(t *testing.T) {
	e := newEnv(t, Options{})
	e.newUser("alice", pwViewer, store.RoleViewer, false)
	c1 := e.login("alice", pwViewer)
	c2 := e.login("alice", pwViewer)
	const newPw = "otra-contraseña-muy-larga-3"

	change := func(cur, next string, c *http.Cookie) *httptest.ResponseRecorder {
		b, _ := json.Marshal(map[string]string{"current_password": cur, "new_password": next})
		return e.do("POST", "/api/v1/auth/password", string(b), withCookie(c))
	}

	// Rechazos: la actual no coincide (y cuenta como fallo), politica, misma contraseña.
	e.wantStatus(change("incorrecta-incorrecta-1", newPw, c1), http.StatusUnauthorized, "invalid_credentials")
	if got := e.count(`SELECT failed_logins FROM users WHERE username = 'alice'`); got != 1 {
		t.Errorf("la actual incorrecta debe contar como fallo, failed_logins got %d", got)
	}
	rec := change(pwViewer, "corta", c1)
	e.wantStatus(rec, http.StatusBadRequest, "weak_password")
	if !strings.Contains(rec.Body.String(), "15") {
		t.Errorf("el mensaje debe explicar el minimo de caracteres: %s", rec.Body.String())
	}
	e.wantStatus(change(pwViewer, strings.Repeat("a", 129), c1), http.StatusBadRequest, "weak_password")
	e.wantStatus(change(pwViewer, pwViewer, c1), http.StatusBadRequest, "weak_password")

	// Exito: 200, las OTRAS sesiones mueren y la actual sigue.
	e.wantStatus(change(pwViewer, newPw, c1), http.StatusOK, "")
	e.wantStatus(e.do("GET", "/api/v1/auth/me", "", withCookie(c2)), http.StatusUnauthorized, "unauthenticated")
	e.wantStatus(e.do("GET", "/api/v1/auth/me", "", withCookie(c1)), http.StatusOK, "")
	e.wantStatus(e.do("POST", "/api/v1/auth/login", loginBody("alice", pwViewer)), http.StatusUnauthorized, "invalid_credentials")
	e.login("alice", newPw)
	if got := e.count(`SELECT count(*) FROM audit_log WHERE event = 'password_changed' AND outcome = 'ok'`); got != 1 {
		t.Errorf("password_changed auditado got %d, want 1", got)
	}
}

func TestMustChangePasswordRestrictsTheSession(t *testing.T) {
	e := newEnv(t, Options{})
	e.newUser("admin", pwAdmin, store.RoleAdmin, true)
	c := e.login("admin", pwAdmin)

	e.wantStatus(e.do("GET", "/api/v1/hosts", "", withCookie(c)), 403, "password_change_required")
	e.wantStatus(e.do("GET", "/api/v1/users", "", withCookie(c)), 403, "password_change_required")
	e.wantStatus(e.do("POST", "/api/v1/hosts/enroll-token", `{"hostname":"x"}`, withCookie(c)), 403, "password_change_required")

	rec := e.do("GET", "/api/v1/auth/me", "", withCookie(c))
	e.wantStatus(rec, http.StatusOK, "")
	var me struct {
		User userDTO `json:"user"`
	}
	e.dataAs(rec, &me)
	if !me.User.MustChangePassword {
		t.Error("me debe informar must_change_password=true")
	}

	b, _ := json.Marshal(map[string]string{"current_password": pwAdmin, "new_password": "contraseña-definitiva-larga-4"})
	e.wantStatus(e.do("POST", "/api/v1/auth/password", string(b), withCookie(c)), http.StatusOK, "")
	e.wantStatus(e.do("GET", "/api/v1/hosts", "", withCookie(c)), http.StatusOK, "")

	// logout tambien esta permitido durante el cambio pendiente.
	e.newUser("bob", pwViewer, store.RoleViewer, true)
	bc := e.login("bob", pwViewer)
	e.wantStatus(e.do("POST", "/api/v1/auth/logout", "", withCookie(bc)), http.StatusOK, "")
}

// --- enroll ---

func TestEnrollTokenFailuresAreIndistinguishable(t *testing.T) {
	e := newEnv(t, Options{})
	admin := e.adminCookie()

	post := func(token string) *httptest.ResponseRecorder {
		return e.do("POST", "/api/v1/agent/enroll", `{"enroll_token":"`+token+`"}`, fromAddr("203.0.113.5:1"))
	}
	create := func() string {
		rec := e.do("POST", "/api/v1/hosts/enroll-token", `{"hostname":"srv"}`, withCookie(admin))
		e.wantStatus(rec, http.StatusCreated, "")
		var d struct {
			EnrollToken string `json:"enroll_token"`
		}
		e.dataAs(rec, &d)
		return d.EnrollToken
	}

	used := create()
	e.wantStatus(post(used), http.StatusCreated, "")
	expired := create()
	mustExec(t, `UPDATE enroll_tokens SET expires_at = now() - interval '1 minute' WHERE token_hash = $1`, hashToken(expired))

	inexistente := post("0123456789abcdef0123456789abcdef")
	reused := post(used)
	exp := post(expired)
	for name, rec := range map[string]*httptest.ResponseRecorder{"inexistente": inexistente, "reusado": reused, "expirado": exp} {
		e.wantStatus(rec, http.StatusUnauthorized, "invalid_token")
		if !bytes.Equal(rec.Body.Bytes(), inexistente.Body.Bytes()) {
			t.Errorf("%s: cuerpo distinto del de token inexistente: %s", name, rec.Body.String())
		}
	}
	// El motivo real solo queda en el log.
	logs := e.logs.String()
	for _, reason := range []string{`"reason":"not_found"`, `"reason":"used"`, `"reason":"expired"`} {
		if !strings.Contains(logs, reason) {
			t.Errorf("el log no registra el motivo %s", reason)
		}
	}
}

func TestEnrollTokenConcurrentRaceHasOneWinner(t *testing.T) {
	e := newEnv(t, Options{})
	admin := e.adminCookie()
	rec := e.do("POST", "/api/v1/hosts/enroll-token", `{"hostname":"srv"}`, withCookie(admin))
	var d struct {
		EnrollToken string `json:"enroll_token"`
	}
	e.dataAs(rec, &d)

	const n = 10
	recs := make([]*httptest.ResponseRecorder, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// IP distinta por goroutine: aqui se prueba la carrera del token, no el limite por IP.
			recs[i] = e.do("POST", "/api/v1/agent/enroll", `{"enroll_token":"`+d.EnrollToken+`"}`,
				fromAddr(fmt.Sprintf("203.0.113.%d:1", i+1)))
		}()
	}
	wg.Wait()

	wins := 0
	var loser []byte
	for _, r := range recs {
		switch r.Code {
		case http.StatusCreated:
			wins++
		case http.StatusUnauthorized:
			if loser == nil {
				loser = r.Body.Bytes()
			} else if !bytes.Equal(loser, r.Body.Bytes()) {
				t.Errorf("los perdedores deben responder igual: %s vs %s", loser, r.Body.String())
			}
		default:
			t.Errorf("status inesperado %d: %s", r.Code, r.Body.String())
		}
	}
	if wins != 1 {
		t.Errorf("ganadores got %d, want 1", wins)
	}
	if got := e.count(`SELECT count(*) FROM hosts`); got != 1 {
		t.Errorf("hosts creados got %d, want 1", got)
	}
}

func TestEnrollTokensAreNeverStoredInPlaintext(t *testing.T) {
	e := newEnv(t, Options{})
	admin := e.adminCookie()
	_, agentToken, enrollToken := e.enrollHost(admin, "srv-01")

	for _, table := range []string{"enroll_tokens", "hosts", "audit_log", "sessions", "users"} {
		for name, secret := range map[string]string{"enroll token": enrollToken, "agent token": agentToken} {
			if got := e.count(fmt.Sprintf(`SELECT count(*) FROM %s t WHERE t::text LIKE '%%' || $1 || '%%'`, table), secret); got != 0 {
				t.Errorf("%s aparece en claro en la tabla %s", name, table)
			}
		}
	}
	if got := e.count(`SELECT count(*) FROM enroll_tokens WHERE token_hash = $1 AND created_by IS NOT NULL AND used_at IS NOT NULL`, hashToken(enrollToken)); got != 1 {
		t.Errorf("el enroll token debe estar hasheado, ligado a created_by y marcado usado")
	}
}

func TestActiveEnrollTokensAreCappedAt50(t *testing.T) {
	e := newEnv(t, Options{})
	admin := e.adminCookie()
	for i := range store.MaxActiveEnrollTokens {
		rec := e.do("POST", "/api/v1/hosts/enroll-token", `{"hostname":"srv"}`, withCookie(admin))
		if rec.Code != http.StatusCreated {
			t.Fatalf("token %d: status %d %s", i, rec.Code, rec.Body.String())
		}
	}
	e.wantStatus(e.do("POST", "/api/v1/hosts/enroll-token", `{"hostname":"srv"}`, withCookie(admin)), http.StatusConflict, "too_many_tokens")
}

func TestEnrollRateLimitPerIP(t *testing.T) {
	e := newEnv(t, Options{})
	ip := fromAddr("203.0.113.50:1")
	for i := range 10 {
		rec := e.do("POST", "/api/v1/agent/enroll", `{"enroll_token":"x"}`, ip)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("intento %d: status %d, want 401", i+1, rec.Code)
		}
	}
	rec := e.do("POST", "/api/v1/agent/enroll", `{"enroll_token":"x"}`, ip)
	e.wantStatus(rec, http.StatusTooManyRequests, "rate_limited")
	if rec.Header().Get("Retry-After") == "" {
		t.Error("falta Retry-After")
	}
	e.wantStatus(e.do("POST", "/api/v1/agent/enroll", `{"enroll_token":"x"}`, fromAddr("203.0.113.51:1")), http.StatusUnauthorized, "invalid_token")
}

// --- agentes: revocacion y limites ---

func TestRevokedHostIsRejectedAndNeverReactivated(t *testing.T) {
	e := newEnv(t, Options{})
	admin := e.adminCookie()
	hostID, agentToken, _ := e.enrollHost(admin, "srv-01")
	e.wantStatus(e.metrics(agentToken), http.StatusOK, "")

	// Solo un admin revoca.
	e.newUser("viewer", pwViewer, store.RoleViewer, false)
	vc := e.login("viewer", pwViewer)
	e.wantStatus(e.do("POST", "/api/v1/hosts/"+hostID+"/revoke", "", withCookie(vc)), http.StatusForbidden, "forbidden")
	e.wantStatus(e.metrics(agentToken), http.StatusOK, "")

	e.wantStatus(e.do("POST", "/api/v1/hosts/"+hostID+"/revoke", "", withCookie(admin)), http.StatusOK, "")
	e.wantStatus(e.do("POST", "/api/v1/hosts/"+hostID+"/revoke", "", withCookie(admin)), http.StatusOK, "") // idempotente

	for range 3 {
		e.clock.Advance(3 * time.Second)
		e.wantStatus(e.metrics(agentToken), http.StatusUnauthorized, "invalid_token")
	}
	if got := e.count(`SELECT count(*) FROM hosts WHERE id = $1 AND status = 'disabled' AND agent_token_revoked_at IS NOT NULL`, hostID); got != 1 {
		t.Errorf("el host debe seguir disabled y revocado tras intentos de ingest")
	}
	rec := e.do("GET", "/api/v1/hosts", "", withCookie(vc))
	if !strings.Contains(rec.Body.String(), `"status":"disabled"`) {
		t.Errorf("la lista debe mostrar el host como disabled: %s", rec.Body.String())
	}

	e.wantStatus(e.do("POST", "/api/v1/hosts/"+anyUUID+"/revoke", "", withCookie(admin)), http.StatusNotFound, "not_found")
	e.wantStatus(e.do("POST", "/api/v1/hosts/no-es-uuid/revoke", "", withCookie(admin)), http.StatusBadRequest, "invalid_body")
	if got := e.count(`SELECT count(*) FROM audit_log WHERE event = 'host_revoked'`); got != 2 {
		t.Errorf("host_revoked auditados got %d, want 2 (las dos revocaciones OK)", got)
	}
}

func TestDisabledHostWithoutRevocationIsRejectedToo(t *testing.T) {
	e := newEnv(t, Options{})
	admin := e.adminCookie()
	hostID, agentToken, _ := e.enrollHost(admin, "srv-01")
	mustExec(t, `UPDATE hosts SET status = 'disabled' WHERE id = $1`, hostID)
	e.wantStatus(e.metrics(agentToken), http.StatusUnauthorized, "invalid_token")
	if got := e.count(`SELECT count(*) FROM hosts WHERE id = $1 AND status = 'disabled'`, hostID); got != 1 {
		t.Error("el host disabled se reactivo")
	}
}

func TestMetricsRateLimitPerHost(t *testing.T) {
	e := newEnv(t, Options{})
	admin := e.adminCookie()
	_, tokenA, _ := e.enrollHost(admin, "srv-a", fromAddr("203.0.113.1:1"))
	_, tokenB, _ := e.enrollHost(admin, "srv-b", fromAddr("203.0.113.2:1"))

	for i := range 5 {
		if rec := e.metrics(tokenA); rec.Code != http.StatusOK {
			t.Fatalf("rafaga %d: status %d, want 200", i+1, rec.Code)
		}
	}
	rec := e.metrics(tokenA)
	e.wantStatus(rec, http.StatusTooManyRequests, "rate_limited")
	if rec.Header().Get("Retry-After") == "" {
		t.Error("falta Retry-After")
	}
	e.wantStatus(e.metrics(tokenB), http.StatusOK, "") // otro host, otro bucket
	e.clock.Advance(2 * time.Second)
	e.wantStatus(e.metrics(tokenA), http.StatusOK, "")
}

func TestAgentAuthFailuresAreAggregatedPerMinute(t *testing.T) {
	e := newEnv(t, Options{})
	for range 7 {
		e.wantStatus(e.metrics("token-invalido"), http.StatusUnauthorized, "invalid_token")
	}
	for i := range 3 {
		e.do("POST", "/api/v1/agent/enroll", `{"enroll_token":"x"}`, fromAddr(fmt.Sprintf("203.0.113.%d:1", i+1)))
	}
	if got := e.count(`SELECT count(*) FROM audit_log`); got != 0 {
		t.Fatalf("no debe haber una fila por request, got %d antes del volcado", got)
	}
	e.api.FlushAgentFailures(context.Background())
	e.api.FlushAgentFailures(context.Background()) // segundo volcado: nada nuevo
	if got := e.count(`SELECT count(*) FROM audit_log WHERE event = 'agent_auth_fail' AND outcome = 'fail' AND (detail->>'count')::int = 7`); got != 1 {
		t.Errorf("agent_auth_fail agregado incorrecto")
	}
	if got := e.count(`SELECT count(*) FROM audit_log WHERE event = 'enroll_fail' AND (detail->>'count')::int = 3`); got != 1 {
		t.Errorf("enroll_fail agregado incorrecto")
	}
	if got := e.count(`SELECT count(*) FROM audit_log`); got != 2 {
		t.Errorf("filas de auditoria got %d, want 2", got)
	}
}

// --- gestion de usuarios ---

func TestUserManagement(t *testing.T) {
	e := newEnv(t, Options{})
	admin := e.adminCookie()

	create := func(user, pass, role string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(map[string]string{"username": user, "password": pass, "role": role})
		return e.do("POST", "/api/v1/users", string(b), withCookie(admin))
	}
	e.wantStatus(create("bob", "corta", "viewer"), 400, "weak_password")
	e.wantStatus(create("bob", pwViewer, "root"), 400, "invalid_body")
	e.wantStatus(create("b", pwViewer, "viewer"), 400, "invalid_body")
	e.wantStatus(create("bob smith", pwViewer, "viewer"), 400, "invalid_body")
	rec := create("bob", pwViewer, "viewer")
	e.wantStatus(rec, http.StatusCreated, "")
	e.wantStatus(create("BOB", pwViewer, "viewer"), http.StatusConflict, "username_taken")
	var created struct {
		User adminUserDTO `json:"user"`
	}
	e.dataAs(rec, &created)
	if !created.User.MustChangePassword || created.User.Role != "viewer" {
		t.Errorf("un usuario nuevo debe nacer con cambio de contraseña pendiente: %+v", created.User)
	}
	if strings.Contains(rec.Body.String(), "argon2") || strings.Contains(rec.Body.String(), "password_hash") {
		t.Error("la respuesta no debe exponer el hash")
	}

	rec = e.do("GET", "/api/v1/users", "", withCookie(admin))
	e.wantStatus(rec, http.StatusOK, "")
	var list struct {
		Items []adminUserDTO `json:"items"`
	}
	e.dataAs(rec, &list)
	if len(list.Items) != 2 {
		t.Errorf("usuarios listados got %d, want 2", len(list.Items))
	}

	bobID := created.User.ID
	patch := func(id, body string) *httptest.ResponseRecorder {
		return e.do("PATCH", "/api/v1/users/"+id, body, withCookie(admin))
	}
	e.wantStatus(patch(bobID, `{}`), 400, "invalid_body")
	e.wantStatus(patch(bobID, `{"role":"root"}`), 400, "invalid_body")
	e.wantStatus(patch(bobID, `{"otro":1}`), 400, "invalid_body")
	e.wantStatus(patch("no-uuid", `{"role":"admin"}`), 400, "invalid_body")
	e.wantStatus(patch(anyUUID, `{"role":"admin"}`), 404, "not_found")
	e.wantStatus(patch(bobID, `{"role":"admin"}`), 200, "")
	e.wantStatus(patch(bobID, `{"role":"admin"}`), 200, "") // idempotente, sin evento de cambio

	// Reset por admin: exige cambiar la contraseña y revoca sus sesiones.
	bc := e.login("bob", pwViewer)
	e.wantStatus(patch(bobID, `{"new_password":"corta"}`), 400, "weak_password")
	e.wantStatus(patch(bobID, `{"new_password":"reset-por-el-admin-larga-5"}`), 200, "")
	e.wantStatus(e.do("GET", "/api/v1/auth/me", "", withCookie(bc)), 401, "unauthenticated")
	bc = e.login("bob", "reset-por-el-admin-larga-5")
	e.wantStatus(e.do("GET", "/api/v1/hosts", "", withCookie(bc)), 403, "password_change_required")

	// Nadie deja el sistema sin admin, ni se resetea a si mismo por esta via.
	admins := e.count(`SELECT count(*) FROM users WHERE role = 'admin'`)
	if admins != 2 {
		t.Fatalf("admins got %d, want 2", admins)
	}
	var adminID string
	if err := testPool.QueryRow(context.Background(), `SELECT id FROM users WHERE username = 'admin'`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	e.wantStatus(patch(adminID, `{"new_password":"reset-a-mi-mismo-larga-6"}`), 400, "invalid_body")
	e.wantStatus(patch(bobID, `{"role":"viewer"}`), 200, "")
	e.wantStatus(patch(adminID, `{"role":"viewer"}`), 409, "last_admin")
	e.wantStatus(patch(adminID, `{"disabled":true}`), 409, "last_admin")

	for event, want := range map[string]int{
		"user_created": 1, "user_role_changed": 2, "user_password_reset": 1,
	} {
		if got := e.count(`SELECT count(*) FROM audit_log WHERE event = $1 AND outcome = 'ok'`, event); got != want {
			t.Errorf("evento %s auditado got %d, want %d", event, got, want)
		}
	}
}

// --- auditoria y logs sin secretos ---

func TestAuditAndLogsNeverContainSecrets(t *testing.T) {
	e := newEnv(t, Options{})
	const (
		ghost    = "fantasma-usuario-secreto"
		ghostPw  = "contraseña-del-fantasma-77"
		wrongPw  = "contraseña-equivocada-88"
		bobNewPw = "contraseña-nueva-de-bob-99"
	)
	admin := e.adminCookie()
	adminCookieValue := admin.Value

	e.do("POST", "/api/v1/auth/login", loginBody(ghost, ghostPw))
	e.do("POST", "/api/v1/auth/login", loginBody("admin", wrongPw))
	e.do("POST", "/api/v1/auth/login", `{"username":"`+ghost+`","password":"`+ghostPw+`","extra":1}`) // 400, no debe loguear el body

	hostID, agentToken, enrollToken := e.enrollHost(admin, "srv-01")
	e.wantStatus(e.metrics(agentToken), 200, "")
	e.metrics("token-agente-invalido-conocido")
	e.do("POST", "/api/v1/agent/enroll", `{"enroll_token":"enroll-invalido-conocido"}`)

	e.newUser("bob", pwViewer, store.RoleViewer, false)
	bc := e.login("bob", pwViewer)
	e.do("POST", "/api/v1/hosts/enroll-token", `{"hostname":"x"}`, withCookie(bc)) // authz_denied
	b, _ := json.Marshal(map[string]string{"current_password": pwViewer, "new_password": bobNewPw})
	e.wantStatus(e.do("POST", "/api/v1/auth/password", string(b), withCookie(bc)), 200, "")
	e.do("POST", "/api/v1/hosts/"+hostID+"/revoke", "", withCookie(admin))
	e.do("POST", "/api/v1/auth/logout", "", withCookie(admin))
	e.api.FlushAgentFailures(context.Background())

	var auditDump string
	if err := testPool.QueryRow(context.Background(), `SELECT coalesce(string_agg(a::text, E'\n'), '') FROM audit_log a`).Scan(&auditDump); err != nil {
		t.Fatal(err)
	}
	if auditDump == "" {
		t.Fatal("la auditoria quedo vacia: el test no verifica nada")
	}
	secrets := map[string]string{
		"contraseña de admin": pwAdmin, "contraseña de bob": pwViewer, "contraseña equivocada": wrongPw,
		"contraseña nueva": bobNewPw, "contraseña del fantasma": ghostPw,
		"enroll token": enrollToken, "agent token": agentToken,
		"cookie de admin": adminCookieValue, "cookie de bob": bc.Value,
		"token de agente invalido": "token-agente-invalido-conocido", "enroll invalido": "enroll-invalido-conocido",
		"hash argon2": "$argon2id$", "cadena de conexion": "postgres://",
	}
	logDump := e.logs.String()
	for name, s := range secrets {
		if strings.Contains(auditDump, s) {
			t.Errorf("auditoria contiene %s", name)
		}
		if strings.Contains(logDump, s) {
			t.Errorf("logs contienen %s", name)
		}
	}
	// El usuario tecleado en un login fallido que no existe no se guarda.
	if strings.Contains(auditDump, ghost) || strings.Contains(logDump, ghost) {
		t.Error("el usuario inexistente de un login fallido aparece en la auditoria o los logs")
	}
	if !strings.Contains(auditDump, "login_fail") || !strings.Contains(auditDump, "authz_denied") ||
		!strings.Contains(auditDump, "host_enrolled") || !strings.Contains(auditDump, "enroll_token_created") {
		t.Errorf("faltan eventos esperados en la auditoria:\n%s", auditDump)
	}
}

func TestAuditTruncatesAndFiltersFields(t *testing.T) {
	e := newEnv(t, Options{})
	e.api.writeAudit(context.Background(), store.AuditEntry{
		ActorUsername: strings.Repeat("ñ", 300), Event: "login_ok", Outcome: store.OutcomeOK,
	}, map[string]any{"reason": strings.Repeat("x", 500), "password": "secreto", "token": "secreto", "count": 3})

	if got := e.count(`SELECT count(*) FROM audit_log WHERE char_length(actor_username) = 128`); got != 1 {
		t.Error("actor_username no se trunco a 128 caracteres")
	}
	if got := e.count(`SELECT count(*) FROM audit_log WHERE detail ? 'password' OR detail ? 'token'`); got != 0 {
		t.Error("una clave fuera de la lista blanca llego al detalle")
	}
	if got := e.count(`SELECT count(*) FROM audit_log WHERE char_length(detail->>'reason') = 128 AND (detail->>'count')::int = 3`); got != 1 {
		t.Error("los valores permitidos deben truncarse a 128 y conservarse los enteros")
	}
}

func mustExec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}
