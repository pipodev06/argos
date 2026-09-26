package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"argos/server/internal/store"
)

// Dispositivos sin agente (kind device) contra Postgres real: CRUD de admin, ambito del viewer, entrega de pings al
// colector y estado derivado del ping.

type deviceView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Address   string `json:"address"`
	Status    string `json:"status"`
	Evaluated bool   `json:"evaluated"`
	Collector *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"collector"`
	Group *struct {
		ID string `json:"id"`
	} `json:"group"`
}

// newDevice crea un dispositivo como admin y devuelve su id.
func (s *scopeEnv) newDevice(body string) string {
	s.t.Helper()
	rec := s.do("POST", "/api/v1/devices", body, withCookie(s.admin))
	s.wantStatus(rec, http.StatusCreated, "")
	var d struct {
		Device deviceView `json:"device"`
	}
	s.dataAs(rec, &d)
	return d.Device.ID
}

func TestDevicesAdminCRUD(t *testing.T) {
	x := newScopeEnv(t)
	body := func(name, addr, collector string) string {
		return fmt.Sprintf(`{"name":%q,"address":%q,"collector_id":%q}`, name, addr, collector)
	}
	post := func(b string, status int, code string) {
		t.Helper()
		x.wantStatus(x.do("POST", "/api/v1/devices", b, withCookie(x.admin)), status, code)
	}

	id := x.newDevice(body("router", "192.168.1.1", x.hostA))
	rec := x.do("GET", "/api/v1/devices/"+id, "", withCookie(x.admin))
	x.wantStatus(rec, http.StatusOK, "")
	var got struct {
		Device deviceView `json:"device"`
	}
	x.dataAs(rec, &got)
	d := got.Device
	if d.Kind != "device" || d.Address != "192.168.1.1" || d.Status != "pending" || !d.Evaluated ||
		d.Collector == nil || d.Collector.ID != x.hostA || d.Collector.Name != "host-a" {
		t.Errorf("dispositivo creado got %+v", d)
	}

	// Validacion: nunca un nombre ni una IP que no se pueda sondear.
	for _, addr := range []string{"router.local", "", "127.0.0.1", "0.0.0.0", "::1", "::", "169.254.169.254", "fe80::1",
		"224.0.0.1", "ff02::1", "255.255.255.255", "::ffff:127.0.0.1", "::ffff:169.254.169.254", "10.0.0.1/24", "fe80::1%eth0", "10.0.0.1; ls"} {
		post(body("x", addr, x.hostA), http.StatusBadRequest, "invalid_body")
	}
	post(body("", "10.0.0.9", x.hostA), http.StatusBadRequest, "invalid_body")
	post(body("x", "10.0.0.9", "no-uuid"), http.StatusBadRequest, "invalid_body")
	post(`{"name":"x","address":"10.0.0.9","collector_id":"`+x.hostA+`","extra":1}`, http.StatusBadRequest, "invalid_body")
	post(body("x", "10.0.0.9", id), http.StatusUnprocessableEntity, "collector_invalid") // un dispositivo no es colector
	post(body("dup", "192.168.1.1", x.hostA), http.StatusConflict, "device_exists")
	post(`{"name":"x","address":"10.0.0.9","collector_id":"`+x.hostA+`","group_id":"`+missingID+`"}`, http.StatusUnprocessableEntity, "group_not_found")
	// IPv4 mapeada en IPv6 se normaliza a IPv4: es la misma direccion.
	post(body("mapped", "::ffff:192.168.1.1", x.hostA), http.StatusConflict, "device_exists")

	// Escrituras solo admin.
	for _, c := range []struct{ method, path, b string }{
		{"POST", "/api/v1/devices", body("v", "10.0.0.7", x.hostA)},
		{"PATCH", "/api/v1/devices/" + id, `{"name":"v"}`},
		{"DELETE", "/api/v1/devices/" + id, ""},
	} {
		x.wantStatus(x.do(c.method, c.path, c.b, withCookie(x.va)), http.StatusForbidden, "")
	}
	x.wantStatus(x.do("POST", "/api/v1/devices", body("v", "10.0.0.7", x.hostA)), http.StatusUnauthorized, "")

	// PATCH: nombre, grupo (y quitarlo con null) y direccion.
	x.wantStatus(x.do("PATCH", "/api/v1/devices/"+id, `{}`, withCookie(x.admin)), http.StatusBadRequest, "invalid_body")
	x.wantStatus(x.do("PATCH", "/api/v1/devices/"+id, `{"name":"core","group_id":"`+x.groupA+`"}`, withCookie(x.admin)), http.StatusOK, "")
	rec = x.do("PATCH", "/api/v1/devices/"+id, `{"group_id":null,"address":"192.168.1.2"}`, withCookie(x.admin))
	x.wantStatus(rec, http.StatusOK, "")
	x.dataAs(rec, &got)
	if got.Device.Name != "core" || got.Device.Group != nil || got.Device.Address != "192.168.1.2" {
		t.Errorf("tras el PATCH got %+v", got.Device)
	}
	x.wantStatus(x.do("PATCH", "/api/v1/devices/"+x.hostA, `{"name":"a"}`, withCookie(x.admin)), http.StatusNotFound, "not_found") // un agente no es dispositivo

	// La lista trae kind en /hosts y los agentes no aparecen en /devices.
	rec = x.do("GET", "/api/v1/hosts?q=core", "", withCookie(x.admin))
	x.wantStatus(rec, http.StatusOK, "")
	if !strings.Contains(rec.Body.String(), `"kind":"device"`) {
		t.Errorf("/hosts no muestra el dispositivo con su kind: %.300s", rec.Body.String())
	}
	rec = x.do("GET", "/api/v1/devices", "", withCookie(x.admin))
	if strings.Contains(rec.Body.String(), "host-a") && !strings.Contains(rec.Body.String(), `"collector"`) {
		t.Errorf("/devices lista un agente: %.300s", rec.Body.String())
	}

	x.wantStatus(x.do("DELETE", "/api/v1/devices/"+id, "", withCookie(x.admin)), http.StatusOK, "")
	x.wantStatus(x.do("GET", "/api/v1/devices/"+id, "", withCookie(x.admin)), http.StatusNotFound, "not_found")
	x.wantStatus(x.do("DELETE", "/api/v1/devices/"+id, "", withCookie(x.admin)), http.StatusNotFound, "not_found")
	x.wantStatus(x.do("DELETE", "/api/v1/devices/"+x.hostA, "", withCookie(x.admin)), http.StatusNotFound, "not_found")

	// Auditoria: sin la direccion.
	rec = x.do("GET", "/api/v1/audit?event=device_created", "", withCookie(x.admin))
	x.wantStatus(rec, http.StatusOK, "")
	if !strings.Contains(rec.Body.String(), "device_created") || strings.Contains(rec.Body.String(), "192.168.1") {
		t.Errorf("auditoria del dispositivo got %.400s", rec.Body.String())
	}
}

func TestDevicesViewerScopeAndRedaction(t *testing.T) {
	x := newScopeEnv(t)
	devA := x.newDevice(fmt.Sprintf(`{"name":"dev-a","address":"10.0.0.1","collector_id":%q,"group_id":%q}`, x.hostB, x.groupA))
	devB := x.newDevice(fmt.Sprintf(`{"name":"dev-b","address":"10.0.0.2","collector_id":%q,"group_id":%q}`, x.hostB, x.groupB))
	devN := x.newDevice(fmt.Sprintf(`{"name":"dev-n","address":"10.0.0.3","collector_id":%q}`, x.hostB))

	// El viewer restringido ve solo el de su grupo, sin address ni colector (el colector esta en otro grupo).
	rec := x.do("GET", "/api/v1/devices", "", withCookie(x.va))
	x.wantStatus(rec, http.StatusOK, "")
	var page struct {
		Items []deviceView `json:"items"`
		Total int          `json:"total"`
	}
	x.dataAs(rec, &page)
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != devA {
		t.Fatalf("lista del viewer de A got %+v", page)
	}
	for _, leak := range []string{"10.0.0", "address", "collector", "host-b", x.hostB, "dev-b", "dev-n"} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Errorf("la lista del viewer filtra %q: %.300s", leak, rec.Body.String())
		}
	}
	rec = x.do("GET", "/api/v1/devices/"+devA, "", withCookie(x.va))
	x.wantStatus(rec, http.StatusOK, "")
	if strings.Contains(rec.Body.String(), "10.0.0.1") || strings.Contains(rec.Body.String(), "collector") {
		t.Errorf("el detalle del viewer filtra address o colector: %.300s", rec.Body.String())
	}

	// Ajeno, sin grupo, inexistente y un id de agente: el mismo 404.
	missing := x.do("GET", "/api/v1/devices/"+missingID, "", withCookie(x.va))
	x.wantStatus(missing, http.StatusNotFound, "not_found")
	for _, id := range []string{devB, devN, x.hostA, "no-es-uuid"} {
		got := x.do("GET", "/api/v1/devices/"+id, "", withCookie(x.va))
		x.wantStatus(got, http.StatusNotFound, "not_found")
		if got.Body.String() != missing.Body.String() {
			t.Errorf("404 de %s got %q, want identico al de un inexistente %q", id, got.Body.String(), missing.Body.String())
		}
	}
	// Viewer con ambito total: los ve todos, y aun asi sin address.
	rec = x.do("GET", "/api/v1/devices", "", withCookie(x.vall))
	x.dataAs(rec, &page)
	if page.Total != 3 || strings.Contains(rec.Body.String(), "10.0.0") {
		t.Errorf("viewer con ambito total got total %d o filtra address: %.300s", page.Total, rec.Body.String())
	}
	// Admin: address y colector.
	rec = x.do("GET", "/api/v1/devices/"+devA, "", withCookie(x.admin))
	if !strings.Contains(rec.Body.String(), "10.0.0.1") || !strings.Contains(rec.Body.String(), "host-b") {
		t.Errorf("el admin debe ver address y colector: %.300s", rec.Body.String())
	}
}

func TestDevicesPaginationCursor(t *testing.T) {
	x := newScopeEnv(t)
	for i := 1; i <= 3; i++ {
		x.newDevice(fmt.Sprintf(`{"name":"d%d","address":"10.0.0.%d","collector_id":%q}`, i, i, x.hostA))
	}
	var names []string
	cursor := ""
	for range 5 {
		rec := x.do("GET", "/api/v1/devices?limit=2"+cursor, "", withCookie(x.admin))
		x.wantStatus(rec, http.StatusOK, "")
		var page struct {
			Items      []deviceView `json:"items"`
			NextCursor *string      `json:"next_cursor"`
			Total      int          `json:"total"`
		}
		x.dataAs(rec, &page)
		for _, it := range page.Items {
			names = append(names, it.Name)
		}
		if page.NextCursor == nil {
			break
		}
		cursor = "&cursor=" + *page.NextCursor
	}
	if strings.Join(names, ",") != "d1,d2,d3" {
		t.Errorf("paginado got %v, want d1,d2,d3", names)
	}
	x.wantStatus(x.do("GET", "/api/v1/devices?cursor=basura", "", withCookie(x.admin)), http.StatusBadRequest, "invalid_cursor")
}

func TestPingChecksEndToEndThroughCollector(t *testing.T) {
	x := newScopeEnv(t)
	col, tok, _ := x.enrollHost(x.admin, "colector")
	_, plainTok, _ := x.enrollHost(x.admin, "sin-dispositivos")
	dev := x.newDevice(fmt.Sprintf(`{"name":"switch","address":"10.20.0.5","collector_id":%q,"group_id":%q}`, col, x.groupA))
	other := x.newDevice(fmt.Sprintf(`{"name":"otro","address":"10.20.0.6","collector_id":%q}`, x.hostA))

	// Un ping por chequeo de grupo (todos) y uno de dispositivo concreto.
	rec := x.do("POST", "/api/v1/service-checks", `{"name":"ping","type":"ping","warn_ms":100,"crit_ms":500}`, withCookie(x.admin))
	x.wantStatus(rec, http.StatusCreated, "")
	var ck struct {
		Check struct {
			ID string `json:"id"`
		} `json:"check"`
	}
	x.dataAs(rec, &ck)
	x.wantStatus(x.do("POST", "/api/v1/service-checks",
		fmt.Sprintf(`{"name":"ping-uno","type":"ping","scope":{"type":"device","device_id":%q}}`, other), withCookie(x.admin)), http.StatusCreated, "")
	// Un tcp sin alcance de dispositivo no admite device; un ping no admite target.
	x.wantStatus(x.do("POST", "/api/v1/service-checks",
		fmt.Sprintf(`{"name":"t","type":"tcp","target_host":"localhost","target_port":22,"scope":{"type":"device","device_id":%q}}`, dev), withCookie(x.admin)), http.StatusBadRequest, "invalid_body")
	x.wantStatus(x.do("POST", "/api/v1/service-checks",
		`{"name":"p2","type":"ping","target_host":"10.0.0.1"}`, withCookie(x.admin)), http.StatusBadRequest, "invalid_body")
	x.wantStatus(x.do("POST", "/api/v1/service-checks",
		fmt.Sprintf(`{"name":"p3","type":"ping","scope":{"type":"device","device_id":%q}}`, missingID), withCookie(x.admin)), http.StatusUnprocessableEntity, "device_not_found")

	// El colector recibe un item por dispositivo suyo (no los de otro colector), con IP literal.
	x.clock.Advance(11 * time.Second)
	r := x.do("POST", "/api/v1/agent/metrics", `{"metrics":{"cpu_pct":1},"checks_rev":"","services":[]}`, withHeader("Authorization", "Bearer "+tok))
	x.wantStatus(r, http.StatusOK, "")
	var resp reportResp
	x.dataAs(r, &resp)
	if resp.Checks == nil || len(resp.Checks.Items) != 1 {
		t.Fatalf("entrega al colector got %+v, want 1 ping", resp.Checks)
	}
	it := resp.Checks.Items[0]
	wantID := store.PingItemID(ck.Check.ID, dev)
	if it["type"] != "ping" || it["target"] != "10.20.0.5" || it["device_id"] != dev || it["id"] != wantID {
		t.Errorf("item de ping got %v, want id %s target 10.20.0.5 device_id %s", it, wantID, dev)
	}
	if _, has := it["target_host"]; has {
		t.Error("un ping no lleva target_host")
	}
	// Un agente que no es colector de ningun dispositivo no recibe pings.
	x.clock.Advance(11 * time.Second)
	r = x.do("POST", "/api/v1/agent/metrics", `{"metrics":{"cpu_pct":1},"checks_rev":""}`, withHeader("Authorization", "Bearer "+plainTok))
	if strings.Contains(r.Body.String(), "ping") {
		t.Errorf("un agente sin dispositivos recibio pings: %.300s", r.Body.String())
	}

	// El resultado del colector cambia el estado del dispositivo (no el del colector).
	now := x.clock.Now().Unix()
	x.clock.Advance(11 * time.Second)
	body := fmt.Sprintf(`{"metrics":{"cpu_pct":1},"services":[{"check_id":%q,"name":"","status":"up","response_ms":4,"detail":"ok","ts":%d}]}`, wantID, now)
	x.wantStatus(x.do("POST", "/api/v1/agent/metrics", body, withHeader("Authorization", "Bearer "+tok)), http.StatusOK, "")
	rec = x.do("GET", "/api/v1/devices/"+dev, "", withCookie(x.admin))
	var got struct {
		Device deviceView `json:"device"`
	}
	x.dataAs(rec, &got)
	if got.Device.Status != "up" {
		t.Errorf("estado del dispositivo tras un ping up got %q, want up", got.Device.Status)
	}
	rec = x.do("GET", "/api/v1/hosts/"+dev+"/service-checks", "", withCookie(x.admin))
	x.wantStatus(rec, http.StatusOK, "")
	if !strings.Contains(rec.Body.String(), `"status":"up"`) || !strings.Contains(rec.Body.String(), `"type":"ping"`) {
		t.Errorf("chequeos del dispositivo got %.400s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "10.20.0.5") {
		t.Errorf("los chequeos del dispositivo exponen la direccion: %.400s", rec.Body.String())
	}
	// Un colector con un ping de dispositivo ajeno lo descarta (sin 4xx: un dato viejo no rompe el reporte).
	x.clock.Advance(11 * time.Second)
	foreign := store.PingItemID(ck.Check.ID, other)
	body = fmt.Sprintf(`{"metrics":{"cpu_pct":1},"services":[{"check_id":%q,"name":"","status":"up","response_ms":4,"detail":"ok","ts":%d}]}`, foreign, x.clock.Now().Unix())
	x.wantStatus(x.do("POST", "/api/v1/agent/metrics", body, withHeader("Authorization", "Bearer "+tok)), http.StatusOK, "")
	if n := x.count(`SELECT count(*) FROM service_check_state WHERE host_id = $1`, other); n != 0 {
		t.Errorf("un colector escribio estado de un dispositivo que no es suyo: %d filas", n)
	}
}

func TestDevicesDisableAndEnableAPI(t *testing.T) {
	x := newScopeEnv(t)
	id := x.newDevice(fmt.Sprintf(`{"name":"router","address":"192.168.1.1","collector_id":%q}`, x.hostA))
	status := func() string {
		t.Helper()
		rec := x.do("GET", "/api/v1/devices/"+id, "", withCookie(x.admin))
		x.wantStatus(rec, http.StatusOK, "")
		var got struct {
			Device deviceView `json:"device"`
		}
		x.dataAs(rec, &got)
		return got.Device.Status
	}
	patch := func(c *http.Cookie, body string, code int, errCode string) {
		t.Helper()
		x.wantStatus(x.do("PATCH", "/api/v1/devices/"+id, body, withCookie(c)), code, errCode)
	}

	patch(x.admin, `{"enabled":false}`, http.StatusOK, "")
	if got := status(); got != "disabled" {
		t.Errorf("estado tras deshabilitar got %q, want disabled", got)
	}
	patch(x.admin, `{"enabled":true}`, http.StatusOK, "")
	if got := status(); got != "pending" {
		t.Errorf("estado tras habilitar got %q, want pending", got)
	}
	patch(x.admin, `{"enabled":"si"}`, http.StatusBadRequest, "invalid_body")
	patch(x.va, `{"enabled":false}`, http.StatusForbidden, "")
	if got := status(); got != "pending" {
		t.Errorf("un viewer no debe poder deshabilitar: estado got %q", got)
	}
}
