package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"argos/server/internal/store"
)

type auditPage struct {
	Items []struct {
		ID            int64           `json:"id"`
		TS            time.Time       `json:"ts"`
		Event         string          `json:"event"`
		Outcome       string          `json:"outcome"`
		ActorUsername *string         `json:"actor_username"`
		TargetName    *string         `json:"target_name"`
		IP            *string         `json:"ip"`
		Detail        json.RawMessage `json:"detail"`
	} `json:"items"`
	NextCursor *string `json:"next_cursor"`
	Total      int     `json:"total"`
}

func (e *env) auditRow(ts time.Time, event, outcome, actor, detail string) {
	e.t.Helper()
	var d any
	if detail != "" {
		d = detail
	}
	_, err := testPool.Exec(context.Background(),
		`INSERT INTO audit_log (ts, event, outcome, actor_username, ip, detail) VALUES ($1, $2, $3, NULLIF($4, ''), '192.0.2.7', $5::jsonb)`,
		ts, event, outcome, actor, d)
	if err != nil {
		e.t.Fatalf("insertando auditoria: %v", err)
	}
}

func (e *env) auditList(cookie *http.Cookie, query string) auditPage {
	e.t.Helper()
	rec := e.do("GET", "/api/v1/audit?"+query, "", withCookie(cookie))
	e.wantStatus(rec, http.StatusOK, "")
	var p auditPage
	e.dataAs(rec, &p)
	return p
}

func rfc(t time.Time) string { return url.QueryEscape(t.Format(time.RFC3339)) }

func TestAuditListEndToEnd(t *testing.T) {
	e := newEnv(t, Options{})
	admin := e.adminCookie()
	e.newUser("viewer", pwViewer, store.RoleViewer, false)
	viewer := e.login("viewer", pwViewer)

	base := time.Now().UTC().Truncate(time.Second).Add(-10 * 24 * time.Hour)
	// Tres filas con el mismo ts (desempate por id) y luego mas antiguas.
	e.auditRow(base, "zt_login_ok", "ok", "Alice", `{"reason":"x"}`)
	e.auditRow(base, "zt_login_fail", "fail", "bob", "")
	e.auditRow(base, "zt_authz_denied", "denied", "", `{"route":"/x"}`)
	e.auditRow(base.Add(-time.Hour), "zt_login_ok", "ok", "alice2", "")
	e.auditRow(base.Add(-48*time.Hour), "zt_group_created", "ok", "carol", `{"name":"g"}`)
	// El login del propio setup tambien deja filas: los casos filtran por actor/evento pa aislarlas.

	t.Run("auth", func(t *testing.T) {
		for _, p := range []string{"/api/v1/audit", "/api/v1/audit/events"} {
			e.wantStatus(e.do("GET", p, ""), http.StatusUnauthorized, "unauthenticated")
			e.wantStatus(e.do("GET", p, "", withCookie(viewer)), http.StatusForbidden, "forbidden")
		}
	})

	t.Run("filtros", func(t *testing.T) {
		cases := []struct {
			name, q string
			want    int
		}{
			{"evento", "event=zt_login_fail", 1},
			{"evento repetido OR", "event=zt_login_ok&event=zt_group_created", 3},
			{"outcome", "outcome=denied&event=zt_authz_denied", 1},
			{"outcome OR", "outcome=fail&outcome=denied&event=zt_login_fail&event=zt_authz_denied", 2},
			{"actor subcadena sin mayusculas", "q=ALICE", 2},
			{"actor sin coincidencia", "q=zzz", 0},
			{"combinado", "event=zt_login_ok&q=alice&outcome=ok", 2},
			{"combinado sin coincidencia", "event=zt_login_fail&q=alice", 0},
			{"rango", "q=carol&from=" + rfc(base.Add(-72*time.Hour)) + "&to=" + rfc(base.Add(-24*time.Hour)), 1},
			{"rango excluye", "q=carol&from=" + rfc(base.Add(-24*time.Hour)), 0},
		}
		for _, c := range cases {
			p := e.auditList(admin, c.q)
			if p.Total != c.want || len(p.Items) != c.want {
				t.Errorf("%s: total=%d items=%d, want %d", c.name, p.Total, len(p.Items), c.want)
			}
		}
	})

	t.Run("item y detail", func(t *testing.T) {
		p := e.auditList(admin, "event=zt_authz_denied")
		if len(p.Items) != 1 {
			t.Fatalf("items = %d, want 1", len(p.Items))
		}
		it := p.Items[0]
		var d map[string]string
		if err := json.Unmarshal(it.Detail, &d); err != nil || d["route"] != "/x" {
			t.Errorf("detail = %s, want route=/x", it.Detail)
		}
		if it.ActorUsername != nil || it.IP == nil || *it.IP != "192.0.2.7" || !it.TS.Equal(base) {
			t.Errorf("item = %+v", it)
		}
		p = e.auditList(admin, "q=bob")
		if string(p.Items[0].Detail) != `{}` {
			t.Errorf("detail sin datos = %s, want {}", p.Items[0].Detail)
		}
	})

	t.Run("busca por nombre e id del objetivo", func(t *testing.T) {
		var id string
		if err := testPool.QueryRow(context.Background(), `SELECT id::text FROM users WHERE username = 'viewer'`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := testPool.Exec(context.Background(),
			`INSERT INTO audit_log (ts, event, outcome, target_type, target_id) VALUES (now(), 'zt_target', 'ok', 'user', $1)`, id); err != nil {
			t.Fatal(err)
		}
		for _, q := range []string{"VIEW", id[:8]} {
			p := e.auditList(admin, "event=zt_target&q="+q)
			if p.Total != 1 || p.Items[0].TargetName == nil || *p.Items[0].TargetName != "viewer" {
				t.Errorf("q=%s: total=%d items=%+v, want 1 con target_name=viewer", q, p.Total, p.Items)
			}
		}
	})

	t.Run("cursor", func(t *testing.T) {
		q := "event=zt_login_ok&event=zt_login_fail&event=zt_authz_denied&event=zt_group_created&limit=2"
		var seen []int64
		cursor := ""
		for range 5 {
			p := e.auditList(admin, q+"&cursor="+url.QueryEscape(cursor))
			if p.Total != 5 {
				t.Fatalf("total = %d, want 5", p.Total)
			}
			for _, it := range p.Items {
				seen = append(seen, it.ID)
			}
			if p.NextCursor == nil {
				break
			}
			cursor = *p.NextCursor
		}
		if len(seen) != 5 {
			t.Fatalf("vistos = %v, want 5", seen)
		}
		uniq := map[int64]bool{}
		for _, id := range seen {
			uniq[id] = true
		}
		if len(uniq) != 5 {
			t.Errorf("ids repetidos: %v", seen)
		}
		// Orden ts DESC, id DESC: las tres del mismo ts van de id mayor a menor.
		if seen[0] <= seen[1] || seen[1] <= seen[2] {
			t.Errorf("orden inesperado: %v", seen)
		}
		// El cursor no sirve con otros filtros ni alterado.
		p := e.auditList(admin, q)
		e.wantStatus(e.do("GET", "/api/v1/audit?event=zt_login_ok&cursor="+url.QueryEscape(*p.NextCursor), "", withCookie(admin)), http.StatusBadRequest, "invalid_cursor")
		e.wantStatus(e.do("GET", "/api/v1/audit?cursor=abc.def", "", withCookie(admin)), http.StatusBadRequest, "invalid_cursor")
	})

	t.Run("invalidos", func(t *testing.T) {
		now := time.Now().UTC()
		for _, q := range []string{
			"outcome=raro", "from=ayer", "to=2026-01-01",
			"from=" + rfc(now) + "&to=" + rfc(now.Add(-time.Hour)),
			"from=" + rfc(now.Add(-400*24*time.Hour)) + "&to=" + rfc(now),
			"q=" + strings.Repeat("a", 129), "event=" + strings.Repeat("e", 65), "event=",
		} {
			e.wantStatus(e.do("GET", "/api/v1/audit?"+q, "", withCookie(admin)), http.StatusBadRequest, "invalid_filter")
		}
		for _, q := range []string{"limit=0", "limit=101", "limit=x"} {
			e.wantStatus(e.do("GET", "/api/v1/audit?"+q, "", withCookie(admin)), http.StatusBadRequest, "invalid_body")
		}
		if p := e.auditList(admin, "limit=1"); len(p.Items) != 1 || p.NextCursor == nil {
			t.Errorf("limit=1: items=%d cursor=%v", len(p.Items), p.NextCursor)
		}
	})

	t.Run("eventos", func(t *testing.T) {
		rec := e.do("GET", "/api/v1/audit/events", "", withCookie(admin))
		e.wantStatus(rec, http.StatusOK, "")
		var d struct {
			Events []string `json:"events"`
		}
		e.dataAs(rec, &d)
		got := strings.Join(d.Events, ",")
		for _, want := range []string{"zt_authz_denied", "zt_group_created", "zt_login_fail", "zt_login_ok"} {
			if !strings.Contains(got, want) {
				t.Errorf("falta el evento %q en %v", want, d.Events)
			}
		}
		for i := 1; i < len(d.Events); i++ {
			if d.Events[i-1] >= d.Events[i] {
				t.Errorf("eventos sin ordenar o repetidos: %v", d.Events)
			}
		}
	})

	t.Run("solo lectura", func(t *testing.T) {
		for _, m := range []string{"POST", "PUT", "PATCH", "DELETE"} {
			rec := e.do(m, "/api/v1/audit", `{}`, withCookie(admin))
			if rec.Code != http.StatusMethodNotAllowed && rec.Code != http.StatusNotFound {
				t.Errorf("%s /audit = %d, want 405 o 404", m, rec.Code)
			}
		}
	})

	t.Run("sin secretos", func(t *testing.T) {
		// Lo que escribe la API pasa por la lista blanca: un secreto en el detalle no llega a la tabla ni a la respuesta.
		e.api.writeAudit(context.Background(), store.AuditEntry{Event: "channel_created", Outcome: store.OutcomeOK},
			map[string]any{"name": "n", "secret": "s3cr3t-hmac", "url": "https://x/?token=abc", "password": "hunter2"})
		body := e.do("GET", "/api/v1/audit?event=channel_created", "", withCookie(admin)).Body.String()
		for _, bad := range []string{"s3cr3t-hmac", "token=abc", "hunter2", pwViewer, "password_hash"} {
			if strings.Contains(body, bad) {
				t.Errorf("la respuesta contiene %q", bad)
			}
		}
		if !strings.Contains(body, `"name"`) {
			t.Errorf("falta el detalle permitido")
		}
	})
}
