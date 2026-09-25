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
)

// Ingest del inventario contra Postgres real.

var (
	invRev1 = strings.Repeat("a", 64)
	invRev2 = strings.Repeat("b", 64)
)

type invEnv struct {
	*env
	hostID, token string
}

func newInvEnv(t *testing.T) *invEnv {
	t.Helper()
	e := newEnv(t, Options{})
	hostID, token, _ := e.enrollHost(e.adminCookie(), "srv-inv")
	e.api.inventoryLimiter = newLimiter(1000, time.Minute) // los tests de validacion mandan decenas de envios
	return &invEnv{env: e, hostID: hostID, token: token}
}

func (e *invEnv) post(token, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.do("POST", "/api/v1/agent/inventory", body, withHeader("Authorization", "Bearer "+token))
}

func (e *invEnv) full(module, rev, items string) string {
	return fmt.Sprintf(`{"module":%q,"rev":%q,"items":%s,"truncated":false,"collected_at":%q}`,
		module, rev, items, e.clock.Now().UTC().Format(time.RFC3339))
}

func (e *invEnv) send(module, rev, items string) int {
	e.t.Helper()
	return e.post(e.token, e.full(module, rev, items)).Code
}

func (e *invEnv) changes(module string) []string {
	e.t.Helper()
	rows, err := testPool.Query(context.Background(),
		`SELECT kind || ':' || key FROM inventory_changes WHERE host_id = $1 AND module = $2 ORDER BY id`, e.hostID, module)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		e.t.Fatal(err)
	}
	return out
}

const (
	port22  = `{"proto":"tcp","bind":"0.0.0.0","port":22,"pid":10,"proc":"sshd","owner_known":true}`
	port80  = `{"proto":"tcp","bind":"127.0.0.1","port":80,"proc":"","owner_known":false}`
	svcRun  = `{"name":"Spooler","display":"Print Spooler","state":"running","start":"auto"}`
	svcStop = `{"name":"Spooler","display":"Print Spooler","state":"stopped","start":"auto"}`
)

func TestInventoryIngestHappyPathPerModule(t *testing.T) {
	e := newInvEnv(t)
	cases := []struct{ module, items string }{
		{"ports", "[" + port22 + "," + port80 + "]"},
		{"services", "[" + svcRun + "]"},
		{"processes", `[{"name":"nginx","count":4,"rss_bytes":123456}]`},
	}
	for _, c := range cases {
		if got := e.send(c.module, invRev1, c.items); got != http.StatusNoContent {
			t.Fatalf("%s: status got %d, want 204", c.module, got)
		}
	}
	if got := e.count(`SELECT count(*) FROM inventory_current WHERE host_id = $1`, e.hostID); got != 3 {
		t.Errorf("fotos got %d, want 3", got)
	}
	var items string
	if err := testPool.QueryRow(context.Background(),
		`SELECT items::text FROM inventory_current WHERE host_id = $1 AND module = 'ports'`, e.hostID).Scan(&items); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(items, "pid") {
		t.Errorf("el pid no se guarda: %s", items)
	}
	if got := e.count(`SELECT count(*) FROM inventory_changes`); got != 0 {
		t.Errorf("la linea base no genera cambios, got %d", got)
	}
}

func TestInventoryHeartbeat(t *testing.T) {
	e := newInvEnv(t)
	hb := func(module, rev string) int {
		return e.post(e.token, fmt.Sprintf(`{"module":%q,"rev":%q}`, module, rev)).Code
	}
	if got := hb("ports", invRev1); got != http.StatusConflict {
		t.Fatalf("latido sin foto got %d, want 409", got)
	}
	e.send("ports", invRev1, "["+port22+"]")
	if got := hb("ports", invRev1); got != http.StatusNoContent {
		t.Errorf("latido con rev igual got %d, want 204", got)
	}
	rec := e.post(e.token, fmt.Sprintf(`{"module":"ports","rev":%q}`, invRev2))
	e.wantStatus(rec, http.StatusConflict, "need_full")
	if got := hb("services", invRev1); got != http.StatusConflict {
		t.Errorf("latido de otro modulo got %d, want 409", got)
	}
	if got := e.post(e.token, fmt.Sprintf(`{"module":"ports","rev":%q,"items":[],"truncated":false}`, invRev1)).Code; got != http.StatusBadRequest {
		t.Errorf("latido con items got %d, want 400", got)
	}
}

func TestInventoryRejectsForbiddenFieldsAndBadInput(t *testing.T) {
	e := newInvEnv(t)
	now := e.clock.Now().UTC()
	tests := []struct {
		name, body string
	}{
		{"cmdline en proceso", e.full("processes", invRev1, `[{"name":"a","count":1,"rss_bytes":1,"cmdline":"x --pw=1"}]`)},
		{"exe en puerto", e.full("ports", invRev1, `[{"proto":"tcp","bind":"0.0.0.0","port":1,"proc":"a","owner_known":true,"exe":"C:\\u"}]`)},
		{"username en servicio", e.full("services", invRev1, `[{"name":"a","display":"","state":"running","start":"auto","username":"root"}]`)},
		{"environ en proceso", e.full("processes", invRev1, `[{"name":"a","count":1,"rss_bytes":1,"environ":["A=1"]}]`)},
		{"host_id en el body", `{"module":"ports","rev":"` + invRev1 + `","host_id":"00000000-0000-0000-0000-000000000000"}`},
		{"modulo desconocido", e.full("patches", invRev1, "[]")},
		{"rev corto", e.full("ports", "abc", "[]")},
		{"rev en mayusculas", e.full("ports", strings.Repeat("A", 64), "[]")},
		{"proto invalido", e.full("ports", invRev1, `[{"proto":"icmp","bind":"0.0.0.0","port":1,"proc":"","owner_known":false}]`)},
		{"bind invalido", e.full("ports", invRev1, `[{"proto":"tcp","bind":"nope","port":1,"proc":"","owner_known":false}]`)},
		{"bind con zona", e.full("ports", invRev1, `[{"proto":"tcp","bind":"fe80::1%eth0","port":1,"proc":"","owner_known":false}]`)},
		{"puerto 0", e.full("ports", invRev1, `[{"proto":"tcp","bind":"::","port":0,"proc":"","owner_known":false}]`)},
		{"puerto 65536", e.full("ports", invRev1, `[{"proto":"tcp","bind":"::","port":65536,"proc":"","owner_known":false}]`)},
		{"estado invalido", e.full("services", invRev1, `[{"name":"a","display":"","state":"exploded","start":"auto"}]`)},
		{"inicio invalido", e.full("services", invRev1, `[{"name":"a","display":"","state":"running","start":"x"}]`)},
		{"nombre con control", e.full("services", invRev1, `[{"name":"a\u0007b","display":"","state":"running","start":"auto"}]`)},
		{"nombre vacio", e.full("services", invRev1, `[{"name":"","display":"","state":"running","start":"auto"}]`)},
		{"nombre de 65", e.full("processes", invRev1, `[{"name":"`+strings.Repeat("a", 65)+`","count":1,"rss_bytes":1}]`)},
		{"display de 129", e.full("services", invRev1, `[{"name":"a","display":"`+strings.Repeat("a", 129)+`","state":"running","start":"auto"}]`)},
		{"conteo cero", e.full("processes", invRev1, `[{"name":"a","count":0,"rss_bytes":1}]`)},
		{"rss negativo", e.full("processes", invRev1, `[{"name":"a","count":1,"rss_bytes":-1}]`)},
		{"items no es arreglo", e.full("ports", invRev1, `{"a":1}`)},
		{"sin items", fmt.Sprintf(`{"module":"ports","rev":%q,"truncated":false,"collected_at":%q}`, invRev1, now.Format(time.RFC3339))},
		{"collected_at futuro", fmt.Sprintf(`{"module":"ports","rev":%q,"items":[],"collected_at":%q}`, invRev1, now.Add(48*time.Hour).Format(time.RFC3339))},
		{"collected_at absurdo", fmt.Sprintf(`{"module":"ports","rev":%q,"items":[],"collected_at":"1999-01-01T00:00:00Z"}`, invRev1)},
		{"json roto", `{"module":`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := e.post(e.token, tt.body)
			e.wantStatus(rec, http.StatusBadRequest, "invalid_body")
		})
	}
	if got := e.count(`SELECT count(*) FROM inventory_current`); got != 0 {
		t.Errorf("un envio rechazado no debe guardar nada, got %d filas", got)
	}
}

func TestInventoryLimits(t *testing.T) {
	e := newInvEnv(t)
	many := func(n int, item func(i int) string) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = item(i)
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
	port := func(i int) string {
		return fmt.Sprintf(`{"proto":"tcp","bind":"0.0.0.0","port":%d,"proc":"","owner_known":false}`, i+1)
	}
	svc := func(i int) string {
		return fmt.Sprintf(`{"name":"s%d","display":"","state":"running","start":"auto"}`, i)
	}
	proc := func(i int) string { return fmt.Sprintf(`{"name":"p%d","count":1,"rss_bytes":1}`, i) }

	if got := e.send("ports", invRev1, many(512, port)); got != http.StatusNoContent {
		t.Errorf("512 puertos got %d, want 204", got)
	}
	if got := e.send("ports", invRev2, many(513, port)); got != http.StatusBadRequest {
		t.Errorf("513 puertos got %d, want 400", got)
	}
	if got := e.send("services", invRev1, many(1024, svc)); got != http.StatusNoContent {
		t.Errorf("1024 servicios got %d, want 204", got)
	}
	if got := e.send("services", invRev2, many(1025, svc)); got != http.StatusBadRequest {
		t.Errorf("1025 servicios got %d, want 400", got)
	}
	if got := e.send("processes", invRev1, many(100, proc)); got != http.StatusNoContent {
		t.Errorf("100 procesos got %d, want 204", got)
	}
	if got := e.send("processes", invRev2, many(101, proc)); got != http.StatusBadRequest {
		t.Errorf("101 procesos got %d, want 400", got)
	}
	// truncated se acepta y se guarda.
	body := fmt.Sprintf(`{"module":"processes","rev":%q,"items":[%s],"truncated":true,"collected_at":%q}`,
		invRev2, proc(1), e.clock.Now().UTC().Format(time.RFC3339))
	if got := e.post(e.token, body).Code; got != http.StatusNoContent {
		t.Fatalf("truncated got %d, want 204", got)
	}
	if got := e.count(`SELECT count(*) FROM inventory_current WHERE module = 'processes' AND truncated`); got != 1 {
		t.Errorf("truncated no se guardo")
	}
	// cuerpo mayor a 256 KiB: 413.
	big := e.full("services", invRev2, fmt.Sprintf(`[{"name":"a","display":%q,"state":"running","start":"auto"}]`, strings.Repeat("x", 300<<10)))
	e.wantStatus(e.post(e.token, big), http.StatusRequestEntityTooLarge, "payload_too_large")
}

func TestInventoryChangesAreRecorded(t *testing.T) {
	e := newInvEnv(t)
	e.send("ports", invRev1, "["+port22+"]")
	e.send("ports", invRev2, "["+port22+","+port80+"]") // aparece 80
	e.send("ports", invRev1, "["+port80+"]")            // desaparece 22
	want := []string{"added:tcp|127.0.0.1|80", "removed:tcp|0.0.0.0|22"}
	if got := e.changes("ports"); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("cambios de puertos got %v, want %v", got, want)
	}

	e.send("services", invRev1, "["+svcRun+"]")
	e.send("services", invRev2, "["+svcStop+"]") // cambia el estado
	if got := e.changes("services"); len(got) != 1 || got[0] != "changed:Spooler" {
		t.Errorf("cambios de servicios got %v, want [changed:Spooler]", got)
	}
	var before, after string
	if err := testPool.QueryRow(context.Background(),
		`SELECT before->>'state', after->>'state' FROM inventory_changes WHERE module = 'services'`).Scan(&before, &after); err != nil {
		t.Fatal(err)
	}
	if before != "running" || after != "stopped" {
		t.Errorf("before/after got %s/%s, want running/stopped", before, after)
	}

	// Procesos: sin historial aunque cambien.
	e.send("processes", invRev1, `[{"name":"a","count":1,"rss_bytes":1}]`)
	e.send("processes", invRev2, `[{"name":"b","count":1,"rss_bytes":1}]`)
	if got := e.count(`SELECT count(*) FROM inventory_changes WHERE module NOT IN ('ports','services')`); got != 0 {
		t.Errorf("procesos no tienen historial, got %d", got)
	}
}

func TestInventoryResendIsIdempotent(t *testing.T) {
	e := newInvEnv(t)
	e.send("ports", invRev1, "["+port22+"]")
	e.send("ports", invRev2, "["+port22+","+port80+"]")
	for range 3 { // reenvio del mismo envio (respuesta perdida)
		if got := e.send("ports", invRev2, "["+port22+","+port80+"]"); got != http.StatusNoContent {
			t.Fatalf("reenvio got %d, want 204", got)
		}
	}
	if got := e.changes("ports"); len(got) != 1 {
		t.Errorf("el reenvio no debe duplicar cambios, got %v", got)
	}
	// Elementos repetidos en un mismo envio se descartan.
	e.send("ports", invRev1, "["+port22+","+port22+"]")
	if got := e.count(`SELECT item_count FROM inventory_current WHERE module = 'ports'`); got != 1 {
		t.Errorf("item_count got %d, want 1 (duplicado descartado)", got)
	}
}

func TestInventoryOverflowFoldsExcessChanges(t *testing.T) {
	e := newInvEnv(t)
	svcs := func(n int) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = fmt.Sprintf(`{"name":"s%d","display":"","state":"running","start":"auto"}`, i)
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
	rev := func(c string) string { return strings.Repeat(c, 64) }
	total := func() int { return e.count(`SELECT count(*) FROM inventory_changes WHERE host_id = $1`, e.hostID) }
	e.send("services", rev("0"), "[]")
	e.send("services", rev("1"), svcs(500)) // +500
	e.send("services", rev("2"), svcs(900)) // +400 = 900
	if got := total(); got != 900 {
		t.Fatalf("filas antes del tope got %d, want 900", got)
	}
	e.send("services", rev("3"), svcs(1024)) // +124: caben 100 (99 + overflow)
	if got := total(); got != 1000 {
		t.Errorf("filas de cambio got %d, want 1000 (tope diario)", got)
	}
	var dropped int
	if err := testPool.QueryRow(context.Background(),
		`SELECT (after->>'dropped')::int FROM inventory_changes WHERE kind = 'overflow'`).Scan(&dropped); err != nil {
		t.Fatal(err)
	}
	if dropped != 25 {
		t.Errorf("dropped got %d, want 25", dropped)
	}
	e.send("services", rev("4"), svcs(500)) // con el tope agotado no se inserta nada mas
	if got := total(); got != 1000 {
		t.Errorf("filas tras agotar el tope got %d, want 1000", got)
	}
}

func TestInventoryAgentCannotWriteAnotherHost(t *testing.T) {
	e := newInvEnv(t)
	admin := e.login("admin", pwAdmin)
	otherID, otherToken, _ := e.enrollHost(admin, "srv-otro")

	// El body con host_id ajeno se rechaza; con el token propio solo se escribe el host propio.
	body := fmt.Sprintf(`{"module":"ports","rev":%q,"host_id":%q,"items":[],"collected_at":%q}`,
		invRev1, e.hostID, e.clock.Now().UTC().Format(time.RFC3339))
	e.wantStatus(e.post(otherToken, body), http.StatusBadRequest, "invalid_body")

	if got := e.post(otherToken, e.full("ports", invRev1, "["+port22+"]")).Code; got != http.StatusNoContent {
		t.Fatalf("envio propio got %d, want 204", got)
	}
	if got := e.count(`SELECT count(*) FROM inventory_current WHERE host_id = $1`, otherID); got != 1 {
		t.Errorf("filas del host del token got %d, want 1", got)
	}
	if got := e.count(`SELECT count(*) FROM inventory_current WHERE host_id = $1`, e.hostID); got != 0 {
		t.Errorf("filas del host ajeno got %d, want 0", got)
	}
	// Token invalido o revocado: 401, sin escribir.
	e.wantStatus(e.post("token-falso", e.full("ports", invRev1, "[]")), http.StatusUnauthorized, "invalid_token")
	if got := e.post("", e.full("ports", invRev1, "[]")).Code; got != http.StatusUnauthorized {
		t.Errorf("sin token got %d, want 401", got)
	}
}

func TestInventoryRateLimit(t *testing.T) {
	e := newInvEnv(t)
	e.api.inventoryLimiter = newLimiter(20, time.Minute)
	limited := 0
	for range 30 {
		rec := e.post(e.token, `{"module":"ports","rev":"`+invRev1+`"}`)
		if rec.Code == http.StatusTooManyRequests {
			limited++
		}
	}
	if limited == 0 {
		t.Errorf("30 envios seguidos deben toparse con el 429")
	}
}

func TestInventoryOwnerUnknownDropsProc(t *testing.T) {
	e := newInvEnv(t)
	e.send("ports", invRev1, `[{"proto":"udp","bind":"::1","port":53,"pid":4,"proc":"algo","owner_known":false}]`)
	var it map[string]any
	var raw []byte
	if err := testPool.QueryRow(context.Background(),
		`SELECT items->0 FROM inventory_current WHERE host_id = $1`, e.hostID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &it); err != nil {
		t.Fatal(err)
	}
	if it["proc"] != "" || it["owner_known"] != false {
		t.Errorf("item got %v, want proc vacio y owner_known false", it)
	}
}
