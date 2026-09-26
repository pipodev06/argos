package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Ingest del modulo updates y permiso can_updates contra Postgres real.

func (e *invEnv) sendUpdates(rev, items string) int {
	e.t.Helper()
	return e.send("updates", rev, items)
}

const okUpdates = `{"pending_total":5,"pending_security":2,"reboot_required":true,"last_installed":"2026-09-20","source":"apt","cache_age_s":3600,"advisories":["USN-1234-1","KB5001234"]}`

func TestUpdatesIngestStoresSnapshotAndDerivesMarks(t *testing.T) {
	e := newInvEnv(t)
	if got := e.sendUpdates(invRev1, okUpdates); got != http.StatusNoContent {
		t.Fatalf("status got %d, want 204", got)
	}
	var total, sec int
	var reboot bool
	var since *time.Time
	var src string
	var adv []string
	err := testPool.QueryRow(context.Background(),
		`SELECT pending_total, pending_security, reboot_required, reboot_since, source, advisories FROM updates_current WHERE host_id = $1`,
		e.hostID).Scan(&total, &sec, &reboot, &since, &src, &adv)
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 || sec != 2 || !reboot || since == nil || src != "apt" || len(adv) != 2 {
		t.Errorf("foto got total=%d sec=%d reboot=%v since=%v src=%s adv=%v", total, sec, reboot, since, src, adv)
	}
	if n := e.count(`SELECT count(*) FROM inventory_current`); n != 0 {
		t.Errorf("updates no debe escribir en inventory_current, got %d filas", n)
	}
	// Latido: rev igual = 204; rev distinto = 409 need_full.
	hb := func(rev string) *httptest.ResponseRecorder {
		return e.post(e.token, fmt.Sprintf(`{"module":"updates","rev":%q}`, rev))
	}
	if got := hb(invRev1).Code; got != http.StatusNoContent {
		t.Errorf("latido got %d, want 204", got)
	}
	rec := hb(invRev2)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "need_full") {
		t.Errorf("latido con otro rev got %d %s, want 409 need_full", rec.Code, rec.Body.String())
	}
}

func TestUpdatesIngestUnknownStaysNull(t *testing.T) {
	e := newInvEnv(t)
	if got := e.sendUpdates(invRev1, `{"pending_total":null,"pending_security":null,"reboot_required":null,"last_installed":null,"source":"none","cache_age_s":null,"advisories":[]}`); got != http.StatusNoContent {
		t.Fatalf("status got %d, want 204", got)
	}
	// Tambien sin los campos: ausente = desconocido, nunca 0.
	if got := e.sendUpdates(invRev2, `{"source":"none"}`); got != http.StatusNoContent {
		t.Fatalf("status sin campos got %d, want 204", got)
	}
	if n := e.count(`SELECT count(*) FROM updates_current WHERE host_id = $1 AND pending_total IS NULL AND pending_security IS NULL
	                    AND reboot_required IS NULL AND last_installed IS NULL AND cache_age_s IS NULL`, e.hostID); n != 1 {
		t.Errorf("filas con todo desconocido got %d, want 1 (null no se convierte en 0)", n)
	}
}

func TestUpdatesIngestRejectsBadInput(t *testing.T) {
	e := newInvEnv(t)
	manyAdv := make([]string, 201)
	for i := range manyAdv {
		manyAdv[i] = fmt.Sprintf(`"KB%07d"`, i)
	}
	future := e.clock.Now().UTC().Add(96 * time.Hour).Format(time.DateOnly) // UTC y con holgura: la fecha se trunca a medianoche
	tests := []struct{ name, items string }{
		{"items lista", `[]`},
		{"items null", `null`},
		{"items texto", `"x"`},
		{"campo desconocido", `{"source":"apt","title":"Actualizacion de seguridad"}`},
		{"contenido sobrante", `{"source":"apt"} {"x":1}`},
		{"source invalido", `{"source":"chocolatey"}`},
		{"source vacio", `{"source":""}`},
		{"total negativo", `{"source":"apt","pending_total":-1}`},
		{"total enorme", `{"source":"apt","pending_total":1000001}`},
		{"total no numerico", `{"source":"apt","pending_total":"5"}`},
		{"seguridad mayor que total", `{"source":"apt","pending_total":1,"pending_security":2}`},
		{"reboot no booleano", `{"source":"apt","reboot_required":"yes"}`},
		{"fecha absurda antigua", `{"source":"apt","last_installed":"1970-01-01"}`},
		{"fecha futura", `{"source":"apt","last_installed":"` + future + `"}`},
		{"fecha mal formada", `{"source":"apt","last_installed":"25/09/2026"}`},
		{"cache_age negativa", `{"source":"apt","cache_age_s":-1}`},
		{"cache_age enorme", `{"source":"apt","cache_age_s":315360001}`},
		{"aviso con espacio", `{"source":"apt","advisories":["KB 1"]}`},
		{"aviso con caracter de control", `{"source":"apt","advisories":["KB1\u0007"]}`},
		{"aviso con salto de linea", `{"source":"apt","advisories":["KB1\n"]}`},
		{"aviso vacio", `{"source":"apt","advisories":[""]}`},
		{"aviso demasiado largo", `{"source":"apt","advisories":["` + strings.Repeat("A", 33) + `"]}`},
		{"demasiados avisos", `{"source":"apt","advisories":[` + strings.Join(manyAdv, ",") + `]}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := e.sendUpdates(invRev1, tc.items); got != http.StatusBadRequest {
				t.Errorf("status got %d, want 400", got)
			}
		})
	}
	if n := e.count(`SELECT count(*) FROM updates_current`); n != 0 {
		t.Errorf("un rechazo no debe guardar nada, got %d filas", n)
	}
	// 200 avisos (el tope) caben en el limite de cuerpo.
	ok := make([]string, 200)
	for i := range ok {
		ok[i] = fmt.Sprintf(`"KB%07d"`, i)
	}
	if got := e.sendUpdates(invRev1, `{"source":"wua","advisories":[`+strings.Join(ok, ",")+`]}`); got != http.StatusNoContent {
		t.Errorf("200 avisos got %d, want 204", got)
	}
	// collected_at fuera de rango.
	body := fmt.Sprintf(`{"module":"updates","rev":%q,"items":{"source":"apt"},"truncated":false,"collected_at":%q}`,
		invRev2, e.clock.Now().Add(-8*24*time.Hour).UTC().Format(time.RFC3339))
	if got := e.post(e.token, body).Code; got != http.StatusBadRequest {
		t.Errorf("collected_at viejo got %d, want 400", got)
	}
	// Token invalido: 401.
	if got := e.post("token-falso", e.full("updates", invRev1, okUpdates)).Code; got != http.StatusUnauthorized {
		t.Errorf("token invalido got %d, want 401", got)
	}
}

func TestUpdatesIngestDedupesAdvisoriesAndKeepsRebootSince(t *testing.T) {
	e := newInvEnv(t)
	if got := e.sendUpdates(invRev1, `{"source":"dnf","reboot_required":true,"advisories":["RHSA-1","A","RHSA-1"]}`); got != http.StatusNoContent {
		t.Fatalf("status got %d", got)
	}
	var adv []string
	var first time.Time
	if err := testPool.QueryRow(context.Background(), `SELECT advisories, reboot_since FROM updates_current WHERE host_id = $1`, e.hostID).Scan(&adv, &first); err != nil {
		t.Fatal(err)
	}
	if strings.Join(adv, ",") != "A,RHSA-1" {
		t.Errorf("advisories got %v, want [A RHSA-1] (ordenados y sin repetir)", adv)
	}
	e.clock.Advance(2 * time.Hour)
	if got := e.sendUpdates(invRev2, `{"source":"dnf","reboot_required":true}`); got != http.StatusNoContent {
		t.Fatalf("status got %d", got)
	}
	var again time.Time
	if err := testPool.QueryRow(context.Background(), `SELECT reboot_since FROM updates_current WHERE host_id = $1`, e.hostID).Scan(&again); err != nil {
		t.Fatal(err)
	}
	if !again.Equal(first) {
		t.Errorf("reboot_since se movio: %v -> %v", first, again)
	}
	e.clock.Advance(time.Hour)
	if got := e.sendUpdates(invRev1, `{"source":"dnf","reboot_required":false}`); got != http.StatusNoContent {
		t.Fatalf("status got %d", got)
	}
	if n := e.count(`SELECT count(*) FROM updates_current WHERE host_id = $1 AND reboot_since IS NULL AND reboot_required = false`, e.hostID); n != 1 {
		t.Errorf("reboot_since debe limpiarse al volver a false, filas got %d", n)
	}
}

func TestCanUpdatesPermissionAPI(t *testing.T) {
	s := newScopeEnv(t)
	var adminUID string
	if err := testPool.QueryRow(t.Context(), `SELECT id FROM users WHERE username = 'admin'`).Scan(&adminUID); err != nil {
		t.Fatal(err)
	}
	s.patchUser(adminUID, `{"can_updates":true}`, http.StatusBadRequest, "invalid_scope")

	before := s.count(`SELECT count(*) FROM audit_log WHERE event = 'user_permissions_changed'`)
	s.patchUser(s.vaID, `{"can_updates":true}`, http.StatusOK, "")
	s.patchUser(s.vaID, `{"can_updates":true}`, http.StatusOK, "") // repetido: no cambia nada, no audita
	if n := s.count(`SELECT count(*) FROM audit_log WHERE event = 'user_permissions_changed'`); n != before+1 {
		t.Errorf("eventos got %d, want %d", n, before+1)
	}
	if n := s.count(`SELECT count(*) FROM audit_log WHERE event = 'user_permissions_changed'
	                   AND detail->>'prev_can_updates' = 'false' AND detail->>'can_updates' = 'true'
	                   AND detail->>'prev_can_inventory' = 'false' AND detail->>'can_inventory' = 'false'`); n != 1 {
		t.Errorf("detalle de auditoria con antes/despues de can_updates got %d filas, want 1", n)
	}

	rec := s.do("GET", "/api/v1/users", "", withCookie(s.admin))
	s.wantStatus(rec, http.StatusOK, "")
	var list struct {
		Items []struct {
			Username     string `json:"username"`
			CanUpdates   bool   `json:"can_updates"`
			CanInventory bool   `json:"can_inventory"`
		} `json:"items"`
	}
	s.dataAs(rec, &list)
	seen := map[string]bool{}
	for _, u := range list.Items {
		seen[u.Username] = u.CanUpdates
		if u.CanInventory {
			t.Errorf("%s: can_inventory no debia cambiar", u.Username)
		}
	}
	if !seen["viewa"] || seen["vall"] || seen["admin"] {
		t.Errorf("can_updates por usuario got %v, want solo viewa", seen)
	}

	// /auth/me lo expone al propio usuario; sin permiso, false.
	me := func(c *http.Cookie) bool {
		rec := s.do("GET", "/api/v1/auth/me", "", withCookie(c))
		s.wantStatus(rec, http.StatusOK, "")
		var body struct {
			User struct {
				CanUpdates bool `json:"can_updates"`
			} `json:"user"`
		}
		s.dataAs(rec, &body)
		return body.User.CanUpdates
	}
	if vaMe, adminMe := me(s.va), me(s.admin); !vaMe || adminMe {
		t.Errorf("/auth/me can_updates: va=%v admin=%v, want true/false", vaMe, adminMe)
	}
	// Quitarlo aplica en la siguiente peticion y un viewer no puede darselo.
	s.patchUser(s.vaID, `{"can_updates":false}`, http.StatusOK, "")
	if me(s.va) {
		t.Error("can_updates=false debe aplicar en la siguiente peticion")
	}
	s.wantStatus(s.do("PATCH", "/api/v1/users/"+s.vaID, `{"can_updates":true}`, withCookie(s.va)), http.StatusForbidden, "")
}
