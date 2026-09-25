package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"argos/server/internal/store"
)

// Integracion API + Postgres real de grupos, etiquetas, lista, resumen, ingest con campos nuevos y bulk.

func (e *env) newGroup(admin *http.Cookie, body string) string {
	e.t.Helper()
	rec := e.do("POST", "/api/v1/groups", body, withCookie(admin))
	e.wantStatus(rec, http.StatusCreated, "")
	var d struct {
		Group struct {
			ID string `json:"id"`
		} `json:"group"`
	}
	e.dataAs(rec, &d)
	return d.Group.ID
}

type listPage struct {
	Items []struct {
		ID       string `json:"id"`
		Hostname string `json:"hostname"`
		Status   string `json:"status"`
		Group    *struct {
			Name string `json:"name"`
		} `json:"group"`
		IP   *string `json:"ip"`
		Disk *struct {
			Pct   float64 `json:"pct"`
			Mount string  `json:"mount"`
		} `json:"disk"`
	} `json:"items"`
	NextCursor *string `json:"next_cursor"`
	Total      int     `json:"total"`
}

func (e *env) list(cookie *http.Cookie, query string) listPage {
	e.t.Helper()
	rec := e.do("GET", "/api/v1/hosts?"+query, "", withCookie(cookie))
	e.wantStatus(rec, http.StatusOK, "")
	var p listPage
	e.dataAs(rec, &p)
	return p
}

func TestFleetManagementEndToEnd(t *testing.T) {
	e := newEnv(t, Options{HostDownAfter: 2 * time.Minute})
	admin := e.adminCookie()
	e.newUser("viewer", pwViewer, store.RoleViewer, false)
	viewer := e.login("viewer", pwViewer)

	servers := e.newGroup(admin, `{"name":"Servidores","always_on":true}`)
	laptops := e.newGroup(admin, `{"name":"Laptops","down_after_seconds":600}`)
	e.wantStatus(e.do("POST", "/api/v1/groups", `{"name":"servidores"}`, withCookie(admin)), http.StatusConflict, "group_exists")

	// Host enrolado directo en un grupo (enroll-token con group_id).
	rec := e.do("POST", "/api/v1/hosts/enroll-token", `{"hostname":"srv-01","group_id":"`+servers+`"}`, withCookie(admin))
	e.wantStatus(rec, http.StatusCreated, "")
	var tok struct {
		EnrollToken string `json:"enroll_token"`
	}
	e.dataAs(rec, &tok)
	rec = e.do("POST", "/api/v1/agent/enroll", `{"enroll_token":"`+tok.EnrollToken+`","os":"linux","arch":"amd64"}`)
	e.wantStatus(rec, http.StatusCreated, "")
	var enr struct {
		HostID     string `json:"host_id"`
		AgentToken string `json:"agent_token"`
	}
	e.dataAs(rec, &enr)
	srv := enr.HostID
	lap, lapTok, _ := e.enrollHost(admin, "lap-01")

	// Un host creado sin grupo queda sin grupo; luego se asigna.
	e.wantStatus(e.do("PUT", "/api/v1/hosts/"+lap+"/group", `{"group_id":"`+laptops+`"}`, withCookie(admin)), http.StatusOK, "")

	// Ingest con el payload NUEVO del agente.
	body := `{"timestamp":` + strconv.FormatInt(e.clock.Now().Unix(), 10) + `,"metrics":{"cpu_pct":91.5,"mem_used_mb":8000,"mem_total_mb":16000,` +
		`"net":{"rx_bytes":1,"tx_bytes":2},"swap":{"used_mb":0,"total_mb":2048},"load":{"l1":0.5,"l5":0.4,"l15":0.3},"battery":{"pct":87,"charging":true}},` +
		`"disks":[{"mount":"/","used_mb":50000,"total_mb":200000},{"mount":"/data","used_mb":90000,"total_mb":100000}],` +
		`"host_info":{"boot_time":` + strconv.FormatInt(e.clock.Now().Add(-time.Hour).Unix(), 10) + `,"cpu_model":"Ryzen","cpu_cores":8,"cpu_threads":16,"os_name":"Ubuntu","os_version":"24.04","kernel_version":"6.8","agent_version":"0.4.1","ips":["127.0.0.1","192.168.1.25"]},"services":[]}`
	e.wantStatus(e.do("POST", "/api/v1/agent/metrics", body, withHeader("Authorization", "Bearer "+enr.AgentToken)), http.StatusOK, "")
	// ...y con el payload VIEJO (otro host): sigue funcionando.
	e.wantStatus(e.metrics(lapTok), http.StatusOK, "")

	// Detalle: grupo, etiquetas, info, latest extendido y discos.
	rec = e.do("GET", "/api/v1/hosts/"+srv, "", withCookie(viewer))
	e.wantStatus(rec, http.StatusOK, "")
	var det struct {
		Host struct{ Group struct{ Name string } }
		Info struct {
			CPUModel   string   `json:"cpu_model"`
			IPs        []string `json:"ips"`
			MemTotalMB int      `json:"mem_total_mb"`
		}
		Latest struct {
			SwapUsedMB      *int `json:"swap_used_mb"`
			Load1           *float64
			BatteryCharging *bool `json:"battery_charging"`
		}
		Disks []struct {
			Mount string
		}
	}
	e.dataAs(rec, &det)
	if det.Host.Group.Name != "Servidores" || det.Info.CPUModel != "Ryzen" || len(det.Info.IPs) != 2 || det.Info.MemTotalMB != 16000 ||
		det.Latest.SwapUsedMB == nil || *det.Latest.SwapUsedMB != 0 || det.Latest.BatteryCharging == nil || !*det.Latest.BatteryCharging || len(det.Disks) != 2 {
		t.Errorf("detalle got %+v", det)
	}
	rec = e.do("GET", "/api/v1/hosts/"+lap, "", withCookie(viewer))
	var old struct {
		Latest struct {
			SwapUsedMB *int `json:"swap_used_mb"`
		}
		Disks []struct{}
	}
	e.dataAs(rec, &old)
	if old.Latest.SwapUsedMB != nil || len(old.Disks) != 0 {
		t.Errorf("agente viejo: swap debe ser null y sin discos: %+v", old)
	}

	// Lista (viewer): filtros, ip, disco.
	p := e.list(viewer, "sort=name")
	if p.Total != 2 || len(p.Items) != 2 || p.NextCursor != nil {
		t.Fatalf("lista got %+v", p)
	}
	if p.Items[1].Hostname != "srv-01" || p.Items[1].Group == nil || p.Items[1].Group.Name != "Servidores" || p.Items[1].IP == nil || *p.Items[1].IP != "192.168.1.25" ||
		p.Items[1].Disk == nil || p.Items[1].Disk.Mount != "/data" || p.Items[1].Disk.Pct != 90 {
		t.Errorf("srv-01 got %+v", p.Items[1])
	}
	// tag= (etiquetas eliminadas) se ignora: no filtra ni falla.
	if got := e.list(viewer, "tag=DEV"); got.Total != 2 {
		t.Errorf("tag= debe ignorarse, got %+v", got)
	}
	if got := e.list(viewer, "group="+servers); got.Total != 1 || got.Items[0].Hostname != "srv-01" {
		t.Errorf("filtro por grupo got %+v", got)
	}
	if got := e.list(viewer, "group=none"); got.Total != 0 {
		t.Errorf("group=none got %+v", got)
	}

	// Paginacion por cursor con limit=1: dos paginas, el total no cambia.
	first := e.list(viewer, "sort=name&limit=1")
	if len(first.Items) != 1 || first.NextCursor == nil || first.Total != 2 {
		t.Fatalf("pagina 1 got %+v", first)
	}
	second := e.list(viewer, "sort=name&limit=1&cursor="+url.QueryEscape(*first.NextCursor))
	if len(second.Items) != 1 || second.NextCursor != nil || second.Items[0].ID == first.Items[0].ID || second.Total != 2 {
		t.Fatalf("pagina 2 got %+v", second)
	}

	// Detector: srv (always_on) sin reportar -> down; lap (grupo laptops, umbral 600 s) sigue up con 5 min de silencio.
	mustExec(t, `UPDATE hosts SET last_seen_at = now() - interval '5 minutes'`)
	stale, err := e.st.MarkStaleHosts(t.Context(), 2*time.Minute)
	if err != nil || len(stale) != 1 || stale[0].Status != "down" {
		t.Fatalf("detector got (%+v, %v), want srv-01 down", stale, err)
	}
	mustExec(t, `UPDATE hosts SET last_seen_at = now() - interval '20 minutes' WHERE id = $1`, lap)
	if stale, _ = e.st.MarkStaleHosts(t.Context(), 2*time.Minute); len(stale) != 1 || stale[0].Status != "offline" {
		t.Fatalf("detector (laptop) got %+v, want offline", stale)
	}

	// Resumen: solo el down cuenta como "requiere atencion".
	rec = e.do("GET", "/api/v1/summary", "", withCookie(viewer))
	e.wantStatus(rec, http.StatusOK, "")
	var sum struct {
		Counts       map[string]int `json:"counts"`
		RecentlyDown []struct {
			Hostname string `json:"hostname"`
		} `json:"recently_down"`
		ByGroup []struct {
			Name  string `json:"name"`
			Total int    `json:"total"`
		} `json:"by_group"`
	}
	e.dataAs(rec, &sum)
	if sum.Counts["down"] != 1 || sum.Counts["offline"] != 1 || sum.Counts["up"] != 0 || len(sum.RecentlyDown) != 1 || sum.RecentlyDown[0].Hostname != "srv-01" || len(sum.ByGroup) != 2 {
		t.Errorf("resumen got %+v", sum)
	}

	// Cambiar always_on del grupo laptops reclasifica el offline a down; borrar un grupo con hosts se bloquea.
	e.wantStatus(e.do("PATCH", "/api/v1/groups/"+laptops, `{"always_on":true}`, withCookie(admin)), http.StatusOK, "")
	if got := e.list(viewer, "status=down"); got.Total != 2 {
		t.Errorf("tras always_on=true down got %d, want 2", got.Total)
	}
	e.wantStatus(e.do("DELETE", "/api/v1/groups/"+laptops, "", withCookie(admin)), http.StatusConflict, "group_not_empty")

	// Bulk: mover ambos a "sin grupo" y luego borrar el grupo vacio.
	ids := fmt.Sprintf(`["%s","%s"]`, srv, lap)
	e.wantStatus(e.do("POST", "/api/v1/hosts/bulk", `{"ids":`+ids+`,"action":"set_group","group_id":null}`, withCookie(admin)), http.StatusOK, "")
	e.wantStatus(e.do("DELETE", "/api/v1/groups/"+laptops, "", withCookie(admin)), http.StatusOK, "")
	if got := e.list(viewer, "status=offline&group=none"); got.Total != 2 {
		t.Errorf("sin grupo y offline got %d, want 2 (down sin always_on se reclasifica)", got.Total)
	}

	// Un viewer no puede escribir; todo lo escrito quedo auditado.
	e.wantStatus(e.do("POST", "/api/v1/hosts/bulk", `{"ids":`+ids+`,"action":"disable"}`, withCookie(viewer)), http.StatusForbidden, "forbidden")
	for _, ev := range []string{"group_created", "group_updated", "group_deleted", "host_group_set", "hosts_bulk"} {
		if e.count(`SELECT count(*) FROM audit_log WHERE event = $1`, ev) == 0 {
			t.Errorf("falta el evento de auditoria %s", ev)
		}
	}
}

// Sin sesion o sin rol, las rutas nuevas no llegan al store (probado con BD real).
func TestNewRoutesRequireSessionAndRole(t *testing.T) {
	e := newEnv(t, Options{})
	e.newUser("viewer", pwViewer, store.RoleViewer, false)
	viewer := e.login("viewer", pwViewer)
	for _, rt := range []struct{ method, path, body string }{
		{"POST", "/api/v1/groups", `{"name":"x"}`},
		{"PUT", "/api/v1/hosts/" + anyUUID + "/group", `{"group_id":null}`},
		{"POST", "/api/v1/hosts/bulk", `{"ids":["` + anyUUID + `"],"action":"disable"}`},
	} {
		e.wantStatus(e.do(rt.method, rt.path, rt.body), http.StatusUnauthorized, "unauthenticated")
		e.wantStatus(e.do(rt.method, rt.path, rt.body, withCookie(viewer)), http.StatusForbidden, "forbidden")
	}
	if e.count(`SELECT count(*) FROM host_groups`) != 0 {
		t.Errorf("no debio crearse ningun grupo")
	}
}

func TestGroupFilterWithTotalAndCursorEndToEnd(t *testing.T) {
	e := newEnv(t, Options{})
	admin := e.adminCookie()
	e.newUser("viewer", pwViewer, store.RoleViewer, false)
	viewer := e.login("viewer", pwViewer)

	// a y b en el grupo; c sin grupo.
	g := e.newGroup(admin, `{"name":"prod"}`)
	for _, name := range []string{"h-a", "h-b", "h-c"} {
		id, _, _ := e.enrollHost(admin, name)
		if name != "h-c" {
			e.wantStatus(e.do("PUT", "/api/v1/hosts/"+id+"/group", `{"group_id":"`+g+`"}`, withCookie(admin)), http.StatusOK, "")
		}
	}
	p1 := e.list(viewer, "group="+g+"&sort=name&dir=asc&limit=1")
	if p1.Total != 2 || len(p1.Items) != 1 || p1.Items[0].Hostname != "h-a" || p1.NextCursor == nil {
		t.Fatalf("pagina 1 got %+v", p1)
	}
	p2 := e.list(viewer, "group="+g+"&sort=name&dir=asc&limit=1&cursor="+*p1.NextCursor)
	if p2.Total != 2 || len(p2.Items) != 1 || p2.Items[0].Hostname != "h-b" || p2.NextCursor != nil {
		t.Errorf("pagina 2 got %+v", p2)
	}
	// El cursor de un filtro no sirve con otro.
	rec := e.do("GET", "/api/v1/hosts?group=none&sort=name&dir=asc&limit=1&cursor="+*p1.NextCursor, "", withCookie(viewer))
	e.wantStatus(rec, http.StatusBadRequest, "invalid_cursor")
}
