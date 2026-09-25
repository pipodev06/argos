package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"argos/server/internal/store"
)

// Permisos por grupo (tanda 1): hosts, resumen, grupos y PATCH /users, contra Postgres real.

// scopeEnv siembra dos grupos (A, B), un host up en cada uno y uno sin grupo, un admin, un viewer restringido a A
// ("viewa") y un viewer que conserva acceso total ("vall").
type scopeEnv struct {
	*env
	admin, va, vall      *http.Cookie
	vaID                 string
	groupA, groupB       string
	hostA, hostB, hostNo string
}

func newScopeEnv(t *testing.T) *scopeEnv {
	t.Helper()
	e := newEnv(t, Options{HostDownAfter: 2 * time.Minute})
	s := &scopeEnv{env: e, admin: e.adminCookie()}
	s.groupA = e.newGroup(s.admin, `{"name":"A"}`)
	s.groupB = e.newGroup(s.admin, `{"name":"B"}`)

	seed := func(name, groupID string) string {
		id, tok, _ := e.enrollHost(s.admin, name)
		if groupID != "" {
			e.wantStatus(e.do("PUT", "/api/v1/hosts/"+id+"/group", `{"group_id":"`+groupID+`"}`, withCookie(s.admin)), http.StatusOK, "")
		}
		e.wantStatus(e.metrics(tok), http.StatusOK, "")
		return id
	}
	s.hostA, s.hostB, s.hostNo = seed("host-a", s.groupA), seed("host-b", s.groupB), seed("host-none", "")

	s.vaID = e.newUser("viewa", pwViewer, store.RoleViewer, false).ID
	e.newUser("vall", pwViewer, store.RoleViewer, false)
	s.va, s.vall = e.login("viewa", pwViewer), e.login("vall", pwViewer)
	s.patchUser(s.vaID, `{"all_groups":false,"group_ids":["`+s.groupA+`"]}`, http.StatusOK, "")
	return s
}

func (s *scopeEnv) patchUser(id, body string, status int, code string) {
	s.t.Helper()
	s.wantStatus(s.do("PATCH", "/api/v1/users/"+id, body, withCookie(s.admin)), status, code)
}

type scopeSummary struct {
	Counts  map[string]int `json:"counts"`
	ByGroup []struct {
		Name  string `json:"name"`
		Total int    `json:"total"`
	} `json:"by_group"`
	TopCPU []struct {
		ID string `json:"id"`
	} `json:"top_cpu"`
	RecentlyDown []struct{} `json:"recently_down"`
}

func (s *scopeEnv) summaryOf(c *http.Cookie) scopeSummary {
	s.t.Helper()
	rec := s.do("GET", "/api/v1/summary", "", withCookie(c))
	s.wantStatus(rec, http.StatusOK, "")
	var sum scopeSummary
	s.dataAs(rec, &sum)
	return sum
}

func sumCounts(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

func TestViewerRestrictedToGroupSeesOnlyIt(t *testing.T) {
	s := newScopeEnv(t)

	// Lista, total y facets: solo A.
	p := s.list(s.va, "sort=name")
	if p.Total != 1 || len(p.Items) != 1 || p.Items[0].ID != s.hostA {
		t.Fatalf("lista got %+v, want solo host-a", p)
	}
	for _, q := range []string{"group=" + s.groupB, "group=none", "group=" + s.groupB + "&group=none", "q=host-b", "status=up&group=" + s.groupB} {
		if got := s.list(s.va, q); got.Total != 0 || len(got.Items) != 0 {
			t.Errorf("filtro %q got %+v, want vacio", q, got)
		}
	}
	if got := s.list(s.va, "group="+s.groupA); got.Total != 1 {
		t.Errorf("filtro por su grupo got %+v, want 1", got)
	}

	// Resumen: conteos, por grupo y rankings solo A.
	sum := s.summaryOf(s.va)
	if sumCounts(sum.Counts) != 1 || len(sum.ByGroup) != 1 || sum.ByGroup[0].Name != "A" || len(sum.TopCPU) != 1 || sum.TopCPU[0].ID != s.hostA {
		t.Errorf("resumen got %+v", sum)
	}

	// Grupos: solo A, sin "sin grupo".
	rec := s.do("GET", "/api/v1/groups", "", withCookie(s.va))
	s.wantStatus(rec, http.StatusOK, "")
	var groups struct {
		Items []struct {
			Name      string `json:"name"`
			HostCount int    `json:"host_count"`
		} `json:"items"`
		Ungrouped struct {
			HostCount int `json:"host_count"`
		} `json:"ungrouped"`
	}
	s.dataAs(rec, &groups)
	if len(groups.Items) != 1 || groups.Items[0].Name != "A" || groups.Items[0].HostCount != 1 || groups.Ungrouped.HostCount != 0 {
		t.Errorf("grupos got %+v", groups)
	}

	// Detalle propio ok; ajeno, sin grupo e inexistente: 404 identico.
	s.wantStatus(s.do("GET", "/api/v1/hosts/"+s.hostA, "", withCookie(s.va)), http.StatusOK, "")
	missing := s.do("GET", "/api/v1/hosts/0f8fad5b-d9cb-469f-a165-70867728950e", "", withCookie(s.va))
	s.wantStatus(missing, http.StatusNotFound, "not_found")
	for _, id := range []string{s.hostB, s.hostNo} {
		rec := s.do("GET", "/api/v1/hosts/"+id, "", withCookie(s.va))
		s.wantStatus(rec, http.StatusNotFound, "not_found")
		if rec.Body.String() != missing.Body.String() {
			t.Errorf("404 de %s got %q, want identico al de un host inexistente %q", id, rec.Body.String(), missing.Body.String())
		}
		s.wantStatus(s.do("GET", "/api/v1/hosts/"+id+"/metrics", "", withCookie(s.va)), http.StatusNotFound, "not_found")
	}
	s.wantStatus(s.do("GET", "/api/v1/hosts/"+s.hostA+"/metrics", "", withCookie(s.va)), http.StatusOK, "")
}

func TestViewerAllGroupsAndAdminSeeEverything(t *testing.T) {
	s := newScopeEnv(t)
	for name, c := range map[string]*http.Cookie{"viewer all_groups": s.vall, "admin": s.admin} {
		if p := s.list(c, ""); p.Total != 3 || len(p.Items) != 3 {
			t.Errorf("%s: lista got %+v, want 3 hosts", name, p)
		}
		if sum := s.summaryOf(c); sumCounts(sum.Counts) != 3 || len(sum.ByGroup) != 3 {
			t.Errorf("%s: resumen got %+v, want 3 hosts en 3 filas (A, B, sin grupo)", name, sum)
		}
		s.wantStatus(s.do("GET", "/api/v1/hosts/"+s.hostNo, "", withCookie(c)), http.StatusOK, "")
		s.wantStatus(s.do("GET", "/api/v1/hosts/"+s.hostB+"/metrics", "", withCookie(c)), http.StatusOK, "")
	}
	rec := s.do("GET", "/api/v1/groups", "", withCookie(s.vall))
	var groups struct {
		Items     []struct{} `json:"items"`
		Ungrouped struct {
			HostCount int `json:"host_count"`
		} `json:"ungrouped"`
	}
	s.dataAs(rec, &groups)
	if len(groups.Items) != 2 || groups.Ungrouped.HostCount != 1 {
		t.Errorf("grupos de viewer all_groups got %+v", groups)
	}
}

func TestScopeChangeAppliesOnNextRequest(t *testing.T) {
	s := newScopeEnv(t)
	if p := s.list(s.va, ""); p.Total != 1 {
		t.Fatalf("antes got %+v", p)
	}

	// Revocar el grupo: la misma sesion deja de ver el host en la siguiente peticion.
	s.patchUser(s.vaID, `{"group_ids":[]}`, http.StatusOK, "")
	if p := s.list(s.va, ""); p.Total != 0 || len(p.Items) != 0 {
		t.Errorf("tras revocar got %+v, want vacio", p)
	}
	s.wantStatus(s.do("GET", "/api/v1/hosts/"+s.hostA, "", withCookie(s.va)), http.StatusNotFound, "not_found")
	if sum := s.summaryOf(s.va); sumCounts(sum.Counts) != 0 || len(sum.ByGroup) != 0 {
		t.Errorf("resumen tras revocar got %+v", sum)
	}

	// Darle B: ve B y ya no A.
	s.patchUser(s.vaID, `{"group_ids":["`+strings.ToUpper(s.groupB)+`"]}`, http.StatusOK, "")
	if p := s.list(s.va, ""); p.Total != 1 || p.Items[0].ID != s.hostB {
		t.Errorf("con B got %+v", p)
	}

	// Volver a todos.
	s.patchUser(s.vaID, `{"all_groups":true}`, http.StatusOK, "")
	if p := s.list(s.va, ""); p.Total != 3 {
		t.Errorf("con todos got %+v, want 3", p)
	}
}

func TestCursorFromAnotherScopeIsRejected(t *testing.T) {
	s := newScopeEnv(t)
	var page struct {
		NextCursor *string `json:"next_cursor"`
	}
	rec := s.do("GET", "/api/v1/hosts?limit=1&sort=name", "", withCookie(s.admin))
	s.wantStatus(rec, http.StatusOK, "")
	s.dataAs(rec, &page)
	if page.NextCursor == nil {
		t.Fatal("el admin debia recibir un cursor")
	}
	// El cursor del admin no sirve pa un viewer restringido...
	s.wantStatus(s.do("GET", "/api/v1/hosts?limit=1&sort=name&cursor="+*page.NextCursor, "", withCookie(s.va)), http.StatusBadRequest, "invalid_cursor")
	// ...ni pa quien ve todo pero es otro ambito distinto del original (restringido -> todos).
	s.patchUser(s.vaID, `{"group_ids":["`+s.groupA+`","`+s.groupB+`"]}`, http.StatusOK, "")
	rec = s.do("GET", "/api/v1/hosts?limit=1&sort=name", "", withCookie(s.va))
	s.wantStatus(rec, http.StatusOK, "")
	s.dataAs(rec, &page)
	if page.NextCursor == nil {
		t.Fatal("el viewer con A y B debia recibir un cursor")
	}
	s.wantStatus(s.do("GET", "/api/v1/hosts?limit=1&sort=name&cursor="+*page.NextCursor, "", withCookie(s.va)), http.StatusOK, "")
	s.patchUser(s.vaID, `{"group_ids":["`+s.groupA+`"]}`, http.StatusOK, "")
	s.wantStatus(s.do("GET", "/api/v1/hosts?limit=1&sort=name&cursor="+*page.NextCursor, "", withCookie(s.va)), http.StatusBadRequest, "invalid_cursor")
}

func TestPatchUserPermissionsValidation(t *testing.T) {
	s := newScopeEnv(t)
	var adminUID string
	if err := testPool.QueryRow(t.Context(), `SELECT id FROM users WHERE username = 'admin'`).Scan(&adminUID); err != nil {
		t.Fatal(err)
	}
	many := make([]string, maxUserGroups+1)
	for i := range many {
		many[i] = `"0f8fad5b-d9cb-469f-a165-` + strings.Repeat("0", 12-len(strconv.Itoa(i))) + strconv.Itoa(i) + `"`
	}

	tests := []struct {
		name   string
		id     string
		body   string
		status int
		code   string
	}{
		{"admin no se restringe (all_groups)", adminUID, `{"all_groups":false}`, http.StatusBadRequest, "invalid_scope"},
		{"admin no recibe inventario", adminUID, `{"can_inventory":true}`, http.StatusBadRequest, "invalid_scope"},
		{"admin no recibe grupos", adminUID, `{"group_ids":["` + s.groupA + `"]}`, http.StatusBadRequest, "invalid_scope"},
		{"group_ids con all_groups=true", s.vaID, `{"all_groups":true,"group_ids":["` + s.groupA + `"]}`, http.StatusBadRequest, "invalid_scope"},
		{"grupo inexistente", s.vaID, `{"group_ids":["0f8fad5b-d9cb-469f-a165-70867728950e"]}`, http.StatusUnprocessableEntity, "group_not_found"},
		{"id mal formado", s.vaID, `{"group_ids":["no-es-uuid"]}`, http.StatusBadRequest, "invalid_body"},
		{"ids repetidos", s.vaID, `{"group_ids":["` + s.groupA + `","` + strings.ToUpper(s.groupA) + `"]}`, http.StatusBadRequest, "invalid_body"},
		{"demasiados grupos", s.vaID, `{"group_ids":[` + strings.Join(many, ",") + `]}`, http.StatusBadRequest, "invalid_body"},
		{"campo desconocido", s.vaID, `{"groups":[]}`, http.StatusBadRequest, "invalid_body"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s.patchUser(tc.id, tc.body, tc.status, tc.code)
		})
	}

	// Nada de lo rechazado cambio el ambito de va (sigue en A) ni el del admin.
	if p := s.list(s.va, ""); p.Total != 1 || p.Items[0].ID != s.hostA {
		t.Errorf("va tras rechazos got %+v, want solo A", p)
	}
	var all bool
	if err := testPool.QueryRow(t.Context(), `SELECT all_groups FROM users WHERE id = $1`, adminUID).Scan(&all); err != nil || !all {
		t.Errorf("admin all_groups got (%v, %v), want true", all, err)
	}

	// Un viewer no puede tocar permisos (ruta de admin).
	s.wantStatus(s.do("PATCH", "/api/v1/users/"+s.vaID, `{"all_groups":true}`, withCookie(s.va)), http.StatusForbidden, "")
}

func TestGetUsersShowsPermissionsAndAudits(t *testing.T) {
	s := newScopeEnv(t)
	rec := s.do("GET", "/api/v1/users", "", withCookie(s.admin))
	s.wantStatus(rec, http.StatusOK, "")
	var list struct {
		Items []struct {
			Username     string   `json:"username"`
			AllGroups    bool     `json:"all_groups"`
			GroupIDs     []string `json:"group_ids"`
			CanInventory bool     `json:"can_inventory"`
		} `json:"items"`
	}
	s.dataAs(rec, &list)
	for _, u := range list.Items {
		switch u.Username {
		case "viewa":
			if u.AllGroups || len(u.GroupIDs) != 1 || u.GroupIDs[0] != s.groupA || u.CanInventory {
				t.Errorf("va got %+v", u)
			}
		case "vall", "admin":
			if !u.AllGroups || u.GroupIDs == nil || len(u.GroupIDs) != 0 || u.CanInventory {
				t.Errorf("%s got %+v, want todos los grupos, lista vacia (no null), sin inventario", u.Username, u)
			}
		}
	}

	// Cada cambio de permisos deja un evento con antes y despues; un PATCH que no cambia nada, no.
	before := s.count(`SELECT count(*) FROM audit_log WHERE event = 'user_permissions_changed'`)
	if before != 1 {
		t.Fatalf("eventos tras la siembra got %d, want 1", before)
	}
	s.patchUser(s.vaID, `{"can_inventory":true}`, http.StatusOK, "")
	s.patchUser(s.vaID, `{"can_inventory":true}`, http.StatusOK, "")
	if n := s.count(`SELECT count(*) FROM audit_log WHERE event = 'user_permissions_changed'`); n != 2 {
		t.Errorf("eventos got %d, want 2 (el PATCH repetido no cambia nada)", n)
	}
	if n := s.count(`SELECT count(*) FROM audit_log WHERE event = 'user_permissions_changed'
	                   AND detail->>'prev_can_inventory' = 'false' AND detail->>'can_inventory' = 'true'
	                   AND (detail->>'group_count')::int = 1 AND detail->'group_ids' @> to_jsonb($1::text)`, s.groupA); n != 1 {
		t.Errorf("detalle de auditoria con antes/despues got %d filas, want 1", n)
	}
}
