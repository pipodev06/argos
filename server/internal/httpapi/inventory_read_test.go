package httpapi

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// Lectura del inventario contra Postgres real: permisos, ambito, cursores, auditoria y aislamiento.

const invBaseRev = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type invReadEnv struct {
	*scopeEnv
}

func newInvReadEnv(t *testing.T) *invReadEnv {
	t.Helper()
	x := &invReadEnv{scopeEnv: newScopeEnv(t)}
	now := time.Now()
	// hostA: puertos con datos y servicios apagado; hostB: procesos.
	mustExec(t, `INSERT INTO inventory_current (host_id, module, rev, items, item_count, truncated, collected_at, received_at)
		VALUES ($1, 'ports', $2, '[{"proto":"tcp","bind":"0.0.0.0","port":22,"proc":"sshd","owner_known":true,"pid":7,"cmdline":"x"}]', 1, true, $3, $3),
		       ($4, 'processes', $2, '[{"name":"nginx","count":2,"rss_bytes":100}]', 1, false, $3, $3)`,
		x.hostA, invBaseRev, now, x.hostB)
	for i := range 5 {
		mustExec(t, `INSERT INTO inventory_changes (host_id, module, kind, key, after, at) VALUES ($1, 'ports', 'added', $2, '{"port":1}', $3)`,
			x.hostA, "tcp|0.0.0.0|"+string(rune('a'+i)), now.Add(time.Duration(-i)*time.Minute))
	}
	mustExec(t, `INSERT INTO inventory_changes (host_id, module, kind, key, before, after, at) VALUES ($1, 'services', 'changed', 'svc', '{}', '{}', $2)`,
		x.hostA, now.Add(-time.Hour))
	x.patchUser(x.vaID, `{"can_inventory":true}`, http.StatusOK, "")
	return x
}

func (x *invReadEnv) get(c *http.Cookie, path string) (int, string) {
	rec := x.do("GET", "/api/v1/hosts/"+path, "", withCookie(c))
	return rec.Code, rec.Body.String()
}

func TestInventoryReadPermissions(t *testing.T) {
	x := newInvReadEnv(t)
	for _, suffix := range []string{"/ports", "/services", "/processes", "/changes"} {
		p := "/inventory" + suffix
		missing, missingBody := x.get(x.admin, missingID+p)
		if missing != http.StatusNotFound {
			t.Fatalf("inexistente %s got %d", p, missing)
		}
		if code, _ := x.get(x.admin, x.hostA+p); code != http.StatusOK {
			t.Errorf("admin %s got %d, want 200", p, code)
		}
		if code, _ := x.get(x.va, x.hostA+p); code != http.StatusOK {
			t.Errorf("viewer con permiso y ambito %s got %d, want 200", p, code)
		}
		if code, body := x.get(x.va, x.hostB+p); code != http.StatusNotFound || body != missingBody {
			t.Errorf("host ajeno %s got %d %q, want 404 identico", p, code, body)
		}
		// vall no tiene can_inventory.
		if code, body := x.get(x.vall, x.hostA+p); code != http.StatusNotFound || body != missingBody {
			t.Errorf("sin permiso %s got %d %q, want 404 identico", p, code, body)
		}
	}
	// El permiso denegado se audita como authz_denied (una vez por usuario y ruta en la ventana).
	if n := x.count(`SELECT count(*) FROM audit_log WHERE event = 'authz_denied' AND actor_username = 'vall'`); n != 4 {
		t.Errorf("authz_denied de vall got %d, want 4 (una por ruta)", n)
	}
	// Un host ajeno con permiso no genera authz_denied (no crear un oraculo).
	if n := x.count(`SELECT count(*) FROM audit_log WHERE event = 'authz_denied' AND actor_username = 'viewa'`); n != 0 {
		t.Errorf("authz_denied de viewa got %d, want 0", n)
	}
}

type invListResp struct {
	Module      string           `json:"module"`
	Status      string           `json:"status"`
	Count       int              `json:"count"`
	Truncated   bool             `json:"truncated"`
	CollectedAt *time.Time       `json:"collected_at"`
	UpdatedAt   *time.Time       `json:"updated_at"`
	Items       []map[string]any `json:"items"`
}

func TestInventoryReadModulesAndStatus(t *testing.T) {
	x := newInvReadEnv(t)

	var l invListResp
	x.dataAs(x.do("GET", "/api/v1/hosts/"+x.hostNo+"/inventory/ports", "", withCookie(x.admin)), &l)
	if l.Status != "no_data" {
		t.Errorf("host sin inventario got %q, want no_data", l.Status)
	}
	x.dataAs(x.do("GET", "/api/v1/hosts/"+x.hostA+"/inventory/ports", "", withCookie(x.admin)), &l)
	if l.Status != "ok" || l.Count != 1 || l.UpdatedAt == nil || l.CollectedAt == nil || len(l.Items) != 1 || l.Items[0]["proc"] != "sshd" || !l.Truncated {
		t.Errorf("ports got %+v", l)
	}
	if _, leaked := l.Items[0]["cmdline"]; leaked {
		t.Error("campo fuera de la lista blanca en la respuesta")
	}
	if _, leaked := l.Items[0]["pid"]; leaked {
		t.Error("pid en la respuesta")
	}
	x.dataAs(x.do("GET", "/api/v1/hosts/"+x.hostA+"/inventory/services", "", withCookie(x.admin)), &l)
	if l.Status != "disabled" || l.Items == nil || len(l.Items) != 0 {
		t.Errorf("services desactivado got %+v, want status disabled e items []", l)
	}
	rec := x.do("GET", "/api/v1/hosts/"+x.hostA+"/inventory/ports", "", withCookie(x.admin))
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control got %q, want no-store", got)
	}
}

func TestInventoryChangesCursorPagination(t *testing.T) {
	x := newInvReadEnv(t)
	type page struct {
		Items []struct {
			ID     int64  `json:"id"`
			Module string `json:"module"`
		} `json:"items"`
		Next *string `json:"next_cursor"`
	}
	base := "/api/v1/hosts/" + x.hostA + "/inventory/changes"
	var seen []int64
	next := ""
	for range 10 {
		q := "?limit=2&module=ports"
		if next != "" {
			q += "&cursor=" + next
		}
		var p page
		x.dataAs(x.do("GET", base+q, "", withCookie(x.va)), &p)
		for _, it := range p.Items {
			if it.Module != "ports" {
				t.Errorf("filtro module=ports devolvio %q", it.Module)
			}
			seen = append(seen, it.ID)
		}
		if p.Next == nil {
			break
		}
		next = *p.Next
	}
	if len(seen) != 5 {
		t.Fatalf("filas paginadas got %d, want 5 (%v)", len(seen), seen)
	}
	for i := 1; i < len(seen); i++ {
		if seen[i] == seen[i-1] {
			t.Errorf("fila repetida entre paginas: %v", seen)
		}
	}

	// El cursor esta atado a filtros, host y ambito: reusarlo en otro contexto es 400.
	var p page
	x.dataAs(x.do("GET", base+"?limit=2&module=ports", "", withCookie(x.va)), &p)
	for name, path := range map[string]string{
		"otro filtro":  base + "?limit=2&module=services&cursor=" + *p.Next,
		"otro host":    "/api/v1/hosts/" + x.hostNo + "/inventory/changes?limit=2&module=ports&cursor=" + *p.Next,
		"otro ambito":  base + "?limit=2&module=ports&cursor=" + *p.Next,
		"cursor roto":  base + "?cursor=abc.def",
		"filtro malo":  base + "?kind=zzz",
		"modulo malo":  base + "?module=processes",
		"limite malo":  base + "?limit=0",
		"cursor largo": base + "?cursor=" + strings.Repeat("a", maxCursorLen+1),
	} {
		c := x.va
		if name == "otro ambito" {
			c = x.admin
		}
		rec := x.do("GET", path, "", withCookie(c))
		want := http.StatusBadRequest
		if name == "otro host" {
			want = http.StatusNotFound // fuera del ambito: el guard responde antes de mirar el cursor
		}
		if rec.Code != want {
			t.Errorf("%s got %d, want %d", name, rec.Code, want)
		}
	}
}

func TestInventoryReadAudit(t *testing.T) {
	x := newInvReadEnv(t)
	for range 3 { // refrescos del frontend y cambio de modulo: un solo evento por (usuario, host)
		x.get(x.va, x.hostA+"/inventory/ports")
	}
	x.get(x.va, x.hostA+"/inventory/services")
	x.get(x.admin, x.hostA+"/inventory/ports")
	if n := x.count(`SELECT count(*) FROM audit_log WHERE event = 'inventory_read' AND actor_username = 'viewa' AND target_id = $1`, x.hostA); n != 1 {
		t.Errorf("eventos de viewa got %d, want 1 (ports y services del mismo host, deduplicados)", n)
	}
	if n := x.count(`SELECT count(*) FROM audit_log WHERE event = 'inventory_read' AND actor_username = 'admin'`); n != 1 {
		t.Errorf("eventos de admin got %d, want 1", n)
	}
	if n := x.count(`SELECT count(*) FROM audit_log WHERE event = 'inventory_read' AND detail->>'module' = 'ports' AND detail::text NOT LIKE '{"module": "ports"}'`); n != 0 {
		t.Errorf("el detalle debe llevar solo el modulo, sin contenido (%d filas distintas)", n)
	}
	// Pasada la ventana se vuelve a registrar.
	// La ventana (1 h) supera la inactividad de sesion (30 min): se avanza en tramos con una peticion
	// que no audita (/auth/me) pa mantener viva la sesion.
	for range 3 {
		x.clock.Advance(invAuditWindow/3 + time.Second)
		x.do("GET", "/api/v1/auth/me", "", withCookie(x.admin))
	}
	x.get(x.admin, x.hostA+"/inventory/ports")
	if n := x.count(`SELECT count(*) FROM audit_log WHERE event = 'inventory_read' AND actor_username = 'admin'`); n != 2 {
		t.Errorf("tras la ventana got %d, want 2", n)
	}
	// Una lectura que responde 404 no se audita como lectura.
	before := x.count(`SELECT count(*) FROM audit_log WHERE event = 'inventory_read'`)
	x.get(x.va, x.hostB+"/inventory/ports")
	if n := x.count(`SELECT count(*) FROM audit_log WHERE event = 'inventory_read'`); n != before {
		t.Errorf("un 404 audito una lectura (%d -> %d)", before, n)
	}
}

func TestInventoryNotInHostViews(t *testing.T) {
	x := newInvReadEnv(t)
	for _, p := range []string{"/api/v1/hosts", "/api/v1/hosts/" + x.hostA, "/api/v1/summary", "/api/v1/hosts/" + x.hostA + "/events", "/api/v1/alerts"} {
		for _, c := range []*http.Cookie{x.va, x.admin} {
			body := x.do("GET", p, "", withCookie(c)).Body.String()
			for _, leak := range []string{"sshd", "nginx", "owner_known", "\"inventory\""} {
				if strings.Contains(body, leak) {
					t.Errorf("GET %s expone %q del inventario: %.200s", p, leak, body)
				}
			}
		}
	}
}
