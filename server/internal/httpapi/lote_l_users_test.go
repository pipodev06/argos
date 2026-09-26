package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"argos/server/internal/store"
)

// Lote L (servicio hospedado): B1 alta de viewers con grupos, B8 permiso can_ack y B11 borrado de usuarios.

// newAPIUser da de alta un usuario por la API (como admin), le baja must_change_password (el helper de login no
// sigue el cambio de contraseña) y devuelve su id y su cookie.
func (x *scopeEnv) newAPIUser(body, username string) (string, *http.Cookie) {
	x.t.Helper()
	rec := x.do("POST", "/api/v1/users", body, withCookie(x.admin))
	x.wantStatus(rec, http.StatusCreated, "")
	var d struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	x.dataAs(rec, &d)
	mustExec(x.t, `UPDATE users SET must_change_password = false WHERE id = $1`, d.User.ID)
	return d.User.ID, x.login(username, pwViewer)
}

func userBody(username, extra string) string {
	if extra != "" {
		extra = "," + extra
	}
	return `{"username":"` + username + `","password":"` + pwViewer + `","role":"viewer"` + extra + `}`
}

func TestCreateViewerIsFailClosedByDefault(t *testing.T) {
	x := newAlertScopeEnv(t)

	// Sin group_ids ni all_groups: no ve NADA de ningun cliente (hosts, resumen, alertas, dispositivos).
	rec := x.do("POST", "/api/v1/users", userBody("nuevo", ""), withCookie(x.admin))
	x.wantStatus(rec, http.StatusCreated, "")
	var created struct {
		User adminUserDTO `json:"user"`
	}
	x.dataAs(rec, &created)
	if u := created.User; u.AllGroups || u.GroupIDs == nil || len(u.GroupIDs) != 0 || u.CanInventory || u.CanUpdates || u.CanAck {
		t.Errorf("viewer nuevo got %+v, want all_groups=false, group_ids=[] y sin permisos", u)
	}
	mustExec(t, `UPDATE users SET must_change_password = false WHERE username = 'nuevo'`)
	nuevo := x.login("nuevo", pwViewer)
	if p := x.list(nuevo, ""); p.Total != 0 || len(p.Items) != 0 {
		t.Errorf("hosts del viewer sin grupos got %+v, want vacio", p)
	}
	if s := x.summaryOf(nuevo); sumCounts(s.Counts) != 0 || len(s.ByGroup) != 0 || len(s.TopCPU) != 0 {
		t.Errorf("resumen del viewer sin grupos got %+v, want vacio", s)
	}
	if p := x.alertsOf(nuevo, ""); p.Total != 0 || len(p.Items) != 0 {
		t.Errorf("alertas del viewer sin grupos got %+v, want vacio", p)
	}
	for _, id := range x.alerts {
		x.wantStatus(x.do("GET", "/api/v1/alerts/"+id, "", withCookie(nuevo)), http.StatusNotFound, "not_found")
	}
	for _, h := range []string{x.hostA, x.hostB, x.hostNo} {
		x.wantStatus(x.do("GET", "/api/v1/hosts/"+h, "", withCookie(nuevo)), http.StatusNotFound, "not_found")
	}
	x.wantStatus(x.do("GET", "/api/v1/channels", "", withCookie(nuevo)), http.StatusForbidden, "forbidden")
}

func TestCreateViewerWithGroupsSeesOnlyThem(t *testing.T) {
	x := newAlertScopeEnv(t)
	_, c := x.newAPIUser(userBody("soloa", `"group_ids":["`+x.groupA+`"],"can_ack":true,"can_updates":true`), "soloa")

	if p := x.list(c, "sort=name"); p.Total != 1 || p.Items[0].ID != x.hostA {
		t.Errorf("hosts got %+v, want solo host-a", p)
	}
	if p := x.alertsOf(c, ""); p.Total != 1 || p.Items[0].Host.ID != x.hostA {
		t.Errorf("alertas got %+v, want solo la de host-a", p)
	}
	if s := x.summaryOf(c); sumCounts(s.Counts) != 1 {
		t.Errorf("resumen got %+v, want 1 host", s.Counts)
	}
	x.wantStatus(x.do("GET", "/api/v1/hosts/"+x.hostB, "", withCookie(c)), http.StatusNotFound, "not_found")

	var me struct {
		User userDTO `json:"user"`
	}
	x.dataAs(x.do("GET", "/api/v1/auth/me", "", withCookie(c)), &me)
	if me.User.AllGroups || !me.User.CanAck || !me.User.CanUpdates || me.User.CanInventory {
		t.Errorf("/auth/me got %+v, want all_groups=false, can_ack, can_updates", me.User)
	}

	// La auditoria del alta lleva el rol, los permisos y los grupos, nunca la contraseña.
	var detail string
	if err := testPool.QueryRow(t.Context(),
		`SELECT detail::text FROM audit_log WHERE event = 'user_created' AND detail->>'role' = 'viewer' ORDER BY id DESC LIMIT 1`).Scan(&detail); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail, x.groupA) || !strings.Contains(detail, `"can_ack": true`) || strings.Contains(detail, pwViewer) {
		t.Errorf("auditoria user_created got %s", detail)
	}
}

func TestCreateViewerAllGroupsOnlyWhenAsked(t *testing.T) {
	x := newAlertScopeEnv(t)
	_, c := x.newAPIUser(userBody("todo", `"all_groups":true`), "todo")
	if p := x.list(c, ""); p.Total != 3 {
		t.Errorf("viewer con all_groups explicito ve %d hosts, want 3", p.Total)
	}
}

func TestCreateUserRejectsBadScope(t *testing.T) {
	x := newAlertScopeEnv(t)
	before := x.count(`SELECT count(*) FROM users`)

	x.wantStatus(x.do("POST", "/api/v1/users", userBody("fantasma", `"group_ids":["`+missingID+`"]`), withCookie(x.admin)),
		http.StatusUnprocessableEntity, "group_not_found")
	for name, body := range map[string]string{
		"all_groups y group_ids": userBody("u1", `"all_groups":true,"group_ids":["`+x.groupA+`"]`),
		"admin con grupos":       `{"username":"u2","password":"` + pwViewer + `","role":"admin","group_ids":["` + x.groupA + `"]}`,
		"admin con can_ack":      `{"username":"u3","password":"` + pwViewer + `","role":"admin","can_ack":true}`,
		"group_ids malformado":   userBody("u4", `"group_ids":["no-uuid"]`),
	} {
		rec := x.do("POST", "/api/v1/users", body, withCookie(x.admin))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status got %d, want 400 (%s)", name, rec.Code, rec.Body.String())
		}
	}
	if after := x.count(`SELECT count(*) FROM users`); after != before {
		t.Errorf("usuarios tras altas rechazadas got %d, want %d (ninguna debe crearse)", after, before)
	}

	// Un admin creado por la API siempre ve todo.
	rec := x.do("POST", "/api/v1/users", `{"username":"admin2","password":"`+pwViewer+`","role":"admin"}`, withCookie(x.admin))
	x.wantStatus(rec, http.StatusCreated, "")
	var d struct {
		User adminUserDTO `json:"user"`
	}
	x.dataAs(rec, &d)
	if !d.User.AllGroups {
		t.Errorf("admin nuevo got all_groups=false, want true")
	}
}

// B8: reconocer alertas con can_ack, siempre dentro del ambito.
func TestCanAckAcknowledgesOnlyWithinScope(t *testing.T) {
	x := newAlertScopeEnv(t)
	ack := func(c *http.Cookie, alertID string) *httptest.ResponseRecorder {
		return x.do("POST", "/api/v1/alerts/"+alertID+"/ack", "", withCookie(c))
	}

	// Sin can_ack: 403 (el permiso es explicito).
	x.wantStatus(ack(x.va, x.alerts[x.hostA]), http.StatusForbidden, "forbidden")

	x.patchUser(x.vaID, `{"can_ack":true}`, http.StatusOK, "")
	var users struct {
		Items []adminUserDTO `json:"items"`
	}
	x.dataAs(x.do("GET", "/api/v1/users", "", withCookie(x.admin)), &users)
	for _, u := range users.Items {
		if u.ID == x.vaID && !u.CanAck {
			t.Errorf("GET /users no refleja can_ack: %+v", u)
		}
	}

	// Fuera de ambito: el mismo 404 que una alerta inexistente, y la alerta ajena no cambia.
	missing := ack(x.va, missingID)
	x.wantStatus(missing, http.StatusNotFound, "not_found")
	for _, h := range []string{x.hostB, x.hostNo} {
		rec := ack(x.va, x.alerts[h])
		x.wantStatus(rec, http.StatusNotFound, "not_found")
		if rec.Body.String() != missing.Body.String() {
			t.Errorf("404 de alerta ajena got %q, want identico a inexistente %q", rec.Body.String(), missing.Body.String())
		}
		if n := x.count(`SELECT count(*) FROM alerts WHERE id = $1 AND state = 'open'`, x.alerts[h]); n != 1 {
			t.Errorf("la alerta ajena de %s cambio de estado", h)
		}
	}

	// Propia: 200, queda a nombre del actor y auditada con el.
	x.wantStatus(ack(x.va, x.alerts[x.hostA]), http.StatusOK, "")
	if n := x.count(`SELECT count(*) FROM alerts WHERE id = $1 AND state = 'acknowledged' AND acknowledged_by_name = 'viewa'`, x.alerts[x.hostA]); n != 1 {
		t.Errorf("la alerta propia no quedo reconocida por viewa")
	}
	if n := x.count(`SELECT count(*) FROM audit_log WHERE event = 'alert_acked' AND actor_username = 'viewa' AND outcome = 'ok'`); n != 1 {
		t.Errorf("auditoria alert_acked de viewa got %d filas, want 1", n)
	}
	// Idempotente y sin poder resolver: resolve sigue siendo de admin.
	x.wantStatus(ack(x.va, x.alerts[x.hostA]), http.StatusOK, "")
	x.wantStatus(x.do("POST", "/api/v1/alerts/"+x.alerts[x.hostA]+"/resolve", "", withCookie(x.va)), http.StatusForbidden, "forbidden")

	// Un viewer de ambito total con can_ack reconoce de cualquier grupo.
	mustExec(t, `UPDATE users SET can_ack = true WHERE username = 'vall'`)
	x.wantStatus(ack(x.vall, x.alerts[x.hostB]), http.StatusOK, "")
	// Un admin no necesita el permiso ni se le puede dar.
	x.wantStatus(ack(x.admin, x.alerts[x.hostNo]), http.StatusOK, "")
	var adminID string
	if err := testPool.QueryRow(t.Context(), `SELECT id::text FROM users WHERE username = 'admin'`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	x.patchUser(adminID, `{"can_ack":true}`, http.StatusBadRequest, "invalid_scope")
}

// B11: borrar usuarios.
func TestDeleteUser(t *testing.T) {
	x := newAlertScopeEnv(t)
	admin2 := x.newUser("admin2", pwAdmin, store.RoleAdmin, false)
	x.wantStatus(x.do("POST", "/api/v1/alerts/"+x.alerts[x.hostB]+"/ack", "", withCookie(x.admin)), http.StatusOK, "")

	// Un viewer con grupos, sesion y auditoria previa (login_ok ya lo escribe el login).
	id, c := x.newAPIUser(userBody("borrable", `"group_ids":["`+x.groupA+`"],"can_ack":true`), "borrable")
	x.wantStatus(x.do("POST", "/api/v1/alerts/"+x.alerts[x.hostA]+"/ack", "", withCookie(c)), http.StatusOK, "")
	if n := x.count(`SELECT count(*) FROM audit_log WHERE actor_user_id = $1`, id); n == 0 {
		t.Fatal("siembra: el usuario debia tener auditoria previa")
	}

	// No admin: 403. Id malformado: 400. Inexistente: 404. A si mismo: 409.
	x.wantStatus(x.do("DELETE", "/api/v1/users/"+id, "", withCookie(x.va)), http.StatusForbidden, "forbidden")
	x.wantStatus(x.do("DELETE", "/api/v1/users/no-uuid", "", withCookie(x.admin)), http.StatusBadRequest, "")
	x.wantStatus(x.do("DELETE", "/api/v1/users/"+missingID, "", withCookie(x.admin)), http.StatusNotFound, "not_found")
	var adminID string
	if err := testPool.QueryRow(t.Context(), `SELECT id::text FROM users WHERE username = 'admin'`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	x.wantStatus(x.do("DELETE", "/api/v1/users/"+adminID, "", withCookie(x.admin)), http.StatusConflict, "self_delete")

	x.wantStatus(x.do("DELETE", "/api/v1/users/"+id, "", withCookie(x.admin)), http.StatusOK, "")
	if n := x.count(`SELECT count(*) FROM users WHERE id = $1`, id); n != 0 {
		t.Errorf("el usuario sigue existiendo")
	}
	if n := x.count(`SELECT count(*) FROM sessions WHERE user_id = $1`, id); n != 0 {
		t.Errorf("quedaron %d sesiones", n)
	}
	if n := x.count(`SELECT count(*) FROM user_groups WHERE user_id = $1`, id); n != 0 {
		t.Errorf("quedaron %d pertenencias a grupos", n)
	}
	// Su sesion ya no sirve.
	x.wantStatus(x.do("GET", "/api/v1/hosts", "", withCookie(c)), http.StatusUnauthorized, "")
	// La auditoria previa se conserva (con el nombre) y la baja queda registrada sin datos sensibles.
	if n := x.count(`SELECT count(*) FROM audit_log WHERE actor_user_id = $1 AND actor_username = 'borrable'`, id); n == 0 {
		t.Errorf("la auditoria previa del usuario borrado se perdio")
	}
	var detail string
	if err := testPool.QueryRow(t.Context(),
		`SELECT detail::text FROM audit_log WHERE event = 'user_deleted' AND target_id = $1`, id).Scan(&detail); err != nil {
		t.Fatalf("falta el evento user_deleted: %v", err)
	}
	if !strings.Contains(detail, "borrable") || strings.Contains(detail, "argon2") || strings.Contains(detail, pwViewer) {
		t.Errorf("detalle de user_deleted got %s", detail)
	}
	// Lo que hizo queda (alerta reconocida): con el nombre y sin usuario.
	if n := x.count(`SELECT count(*) FROM alerts WHERE acknowledged_by IS NULL AND acknowledged_by_name = 'borrable'`); n != 1 {
		t.Errorf("alertas reconocidas por el usuario borrado got %d, want 1", n)
	}

	// Un admin puede borrar a otro admin (el actor queda activo); lo creado por el queda sin autor.
	x.wantStatus(x.do("POST", "/api/v1/alert-rules", strings.Replace(cpuRuleBody, "CPU alta", "regla-de-admin2", 1), withCookie(x.admin)), http.StatusCreated, "")
	mustExec(t, `UPDATE alert_rules SET created_by = $1 WHERE name = 'regla-de-admin2'`, admin2.ID)
	x.wantStatus(x.do("DELETE", "/api/v1/users/"+admin2.ID, "", withCookie(x.admin)), http.StatusOK, "")
	if n := x.count(`SELECT count(*) FROM alert_rules WHERE name = 'regla-de-admin2' AND created_by IS NULL`); n != 1 {
		t.Errorf("la regla del usuario borrado debia quedar sin autor")
	}
}
