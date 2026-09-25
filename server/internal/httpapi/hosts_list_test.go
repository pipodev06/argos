package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"argos/server/internal/store"
)

// repeatParam arma "name=v0&name=v1&..." con n valores.
func repeatParam(name string, n int, val func(i int) string) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = name + "=" + val(i)
	}
	return strings.Join(parts, "&")
}

// uuidN devuelve un uuid valido y distinto por n (n < 100).
func uuidN(n int) string { return fmt.Sprintf("00000000-0000-4000-8000-0000000000%02d", n) }

func TestListHostsQueryParsing(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", 129)
	tests := []struct {
		name     string
		query    string
		wantCode string
		check    func(t *testing.T, f store.HostFilter)
	}{
		{"defaults", "", "", func(t *testing.T, f store.HostFilter) {
			if f.Sort != "severity" || f.Desc || f.Limit != 50 || f.After != nil || f.Q != "" || len(f.Statuses) != 0 {
				t.Errorf("defaults got %+v", f)
			}
		}},
		{"q", "q=%20SRV%20", "", func(t *testing.T, f store.HostFilter) {
			if f.Q != "SRV" {
				t.Errorf("q got %q", f.Q)
			}
		}},
		{"status repetido y con comas", "status=up&status=down,offline&status=up", "", func(t *testing.T, f store.HostFilter) {
			if !slices.Equal(f.Statuses, []string{"down", "offline", "up"}) {
				t.Errorf("statuses got %v", f.Statuses)
			}
		}},
		{"group id", "group=" + groupUUID, "", func(t *testing.T, f store.HostFilter) {
			if !slices.Equal(f.GroupIDs, []string{groupUUID}) || f.NoGroup {
				t.Errorf("group got %+v", f)
			}
		}},
		{"group none", "group=none", "", func(t *testing.T, f store.HostFilter) {
			if !f.NoGroup || len(f.GroupIDs) != 0 {
				t.Errorf("group got %+v", f)
			}
		}},
		{"os", "os=Windows", "", func(t *testing.T, f store.HostFilter) {
			if !slices.Equal(f.OSes, []string{"windows"}) {
				t.Errorf("os got %v", f.OSes)
			}
		}},
		{"varios group con none, repetidos y orden", "group=" + uuidN(2) + "&group=none&group=" + groupUUID + "&group=" + groupUUID, "", func(t *testing.T, f store.HostFilter) {
			if !slices.Equal(f.GroupIDs, []string{uuidN(2), groupUUID}) || !f.NoGroup {
				t.Errorf("group got %+v", f)
			}
		}},
		{"varios os", "os=linux&os=Windows&os=linux&os=darwin", "", func(t *testing.T, f store.HostFilter) {
			if !slices.Equal(f.OSes, []string{"darwin", "linux", "windows"}) {
				t.Errorf("os got %v", f.OSes)
			}
		}},
		{"20 group", repeatParam("group", 20, func(i int) string { return uuidN(i) }), "", func(t *testing.T, f store.HostFilter) {
			if len(f.GroupIDs) != 20 {
				t.Errorf("group got %d", len(f.GroupIDs))
			}
		}},
		{"21 group", repeatParam("group", 21, func(i int) string { return uuidN(i) }), "invalid_filter", nil},
		{"21 os", repeatParam("os", 21, func(int) string { return "linux" }), "invalid_filter", nil},
		{"group vacio", "group=", "invalid_filter", nil},
		{"group malo entre buenos", "group=none&group=xyz", "invalid_filter", nil},
		{"os vacio", "os=", "invalid_filter", nil},
		{"os fuera de vocabulario", "os=linux&os=freebsd", "invalid_filter", nil},
		{"dir por defecto segun el orden", "sort=cpu", "", func(t *testing.T, f store.HostFilter) {
			if !f.Desc {
				t.Errorf("cpu debe ir descendente por defecto")
			}
		}},
		{"dir explicito", "sort=cpu&dir=asc", "", func(t *testing.T, f store.HostFilter) {
			if f.Desc {
				t.Errorf("dir=asc ignorado")
			}
		}},
		{"limit", "limit=100", "", func(t *testing.T, f store.HostFilter) {
			if f.Limit != 100 {
				t.Errorf("limit got %d", f.Limit)
			}
		}},

		{"filtros de metricas", "cpu_gt=80.5&mem_gt=0&disk_gt=100&battery_lt=20&seen_before=60", "", func(t *testing.T, f store.HostFilter) {
			if f.CPUGt == nil || *f.CPUGt != 80.5 || f.MemGt == nil || *f.MemGt != 0 || f.DiskGt == nil || *f.DiskGt != 100 ||
				f.BatteryLt == nil || *f.BatteryLt != 20 || f.SeenBefore != 60 {
				t.Errorf("filtros got %+v", f)
			}
		}},
		{"sin filtros de metricas", "", "", func(t *testing.T, f store.HostFilter) {
			if f.CPUGt != nil || f.MemGt != nil || f.DiskGt != nil || f.BatteryLt != nil || f.SeenBefore != 0 {
				t.Errorf("filtros got %+v", f)
			}
		}},
		{"seen_before maximo", "seen_before=31536000", "", func(t *testing.T, f store.HostFilter) {
			if f.SeenBefore != 31536000 {
				t.Errorf("seen_before got %d", f.SeenBefore)
			}
		}},

		{"cpu_gt texto", "cpu_gt=abc", "invalid_filter", nil},
		{"cpu_gt negativo", "cpu_gt=-1", "invalid_filter", nil},
		{"mem_gt sobre 100", "mem_gt=100.1", "invalid_filter", nil},
		{"disk_gt NaN", "disk_gt=NaN", "invalid_filter", nil},
		{"disk_gt Inf", "disk_gt=Inf", "invalid_filter", nil},
		{"battery_lt sobre 100", "battery_lt=101", "invalid_filter", nil},
		{"seen_before bajo", "seen_before=59", "invalid_filter", nil},
		{"seen_before alto", "seen_before=31536001", "invalid_filter", nil},
		{"seen_before decimal", "seen_before=60.5", "invalid_filter", nil},
		{"status invalido", "status=roto", "invalid_body", nil},
		{"group invalido", "group=xyz", "invalid_filter", nil},
		{"sort invalido", "sort=uptime", "invalid_sort", nil},
		{"dir invalido", "dir=arriba", "invalid_body", nil},
		{"limit cero", "limit=0", "invalid_body", nil},
		{"limit enorme", "limit=101", "invalid_body", nil},
		{"limit no numerico", "limit=x", "invalid_body", nil},
		{"q larga", "q=" + long, "invalid_body", nil},
		{"q con control", "q=a%00b", "invalid_body", nil},
		{"os largo", "os=" + strings.Repeat("x", 65), "invalid_filter", nil},
		{"cursor basura", "cursor=basura", "invalid_cursor", nil},
		{"cursor enorme", "cursor=" + strings.Repeat("a", 3000), "invalid_cursor", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := &fakeStore{}
			h, _ := newTestAPI(t, fs)
			rec, resp := do(t, h, "GET", "/api/v1/hosts?"+tt.query, "", "", asSession)
			if tt.wantCode != "" {
				wantError(t, rec, resp, 400, tt.wantCode)
				return
			}
			if rec.Code != 200 {
				t.Fatalf("status %d (%s)", rec.Code, rec.Body.String())
			}
			tt.check(t, fs.x.gotFilter)
		})
	}
}

func TestListHostsResponseShape(t *testing.T) {
	t.Parallel()
	seen := fixedNow.Add(-time.Minute)
	boot := fixedNow.Add(-48 * time.Hour)
	cpu, disk, mount, bp := 12.34, 88.86, "C:", 87
	used, total := 4096, 16000
	osName, osVersion := "Windows 11", "23H2"
	charging := true
	rows := []store.HostRow{
		{ID: "id-1", Hostname: "laptop", Status: "up", StatusChangedAt: seen, LastSeenAt: &seen, Group: &store.GroupRef{ID: groupUUID, Name: "Laptops"},
			OS: "windows", OSName: &osName, OSVersion: &osVersion,
			IPs: []netip.Addr{netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("192.168.1.25")}, CPUPct: &cpu, MemUsedMB: &used, MemTotalMB: &total,
			DiskPct: &disk, DiskMount: &mount, BootTime: &boot, BatteryPct: &bp, BatteryCharging: &charging},
		{ID: "id-2", Hostname: "vacio", Status: "pending", StatusChangedAt: seen},
	}
	fs := viewerStore(&fakeStore{x: fakeExtra{page: store.HostPage{
		Items: rows, Total: 2, NextCursor: &store.HostCursor{NullFlag: 0, SortKey: "3", LName: "laptop", ID: groupUUID},
	}}})
	h, _ := newTestAPI(t, fs)
	rec, resp := do(t, h, "GET", "/api/v1/hosts", "", "", asSession)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var data struct {
		Items      []map[string]any `json:"items"`
		NextCursor *string          `json:"next_cursor"`
		Total      int              `json:"total"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Total != 2 || len(data.Items) != 2 || data.NextCursor == nil || *data.NextCursor == "" {
		t.Fatalf("data got %+v", data)
	}
	keys := []string{"id", "hostname", "status", "status_changed_at", "last_seen_at", "group", "os", "os_name", "os_version", "ip", "cpu_pct", "mem_pct", "disk", "boot_time", "battery"}
	wantKeys(t, "item", data.Items[0], keys...)
	wantKeys(t, "item vacio", data.Items[1], keys...)
	full := data.Items[0]
	if full["ip"] != "192.168.1.25" || full["cpu_pct"] != 12.3 || full["mem_pct"] != 25.6 || full["os"] != "windows" || full["os_name"] != "Windows 11" || full["os_version"] != "23H2" {
		t.Errorf("item got %v (ip = primera no loopback; cpu 1 decimal; mem 4096/16000 = 25.6)", full)
	}
	wantKeys(t, "disk", full["disk"].(map[string]any), "pct", "mount")
	if full["disk"].(map[string]any)["pct"] != 88.9 {
		t.Errorf("disk pct got %v, want 88.9", full["disk"])
	}
	wantKeys(t, "battery", full["battery"].(map[string]any), "pct", "charging")
	wantKeys(t, "group", full["group"].(map[string]any), "id", "name")
	if full["boot_time"] != "2026-09-21T12:00:00Z" {
		t.Errorf("boot_time got %v", full["boot_time"])
	}
	empty := data.Items[1]
	for _, k := range []string{"group", "os_name", "os_version", "ip", "cpu_pct", "mem_pct", "disk", "boot_time", "battery", "last_seen_at"} {
		if empty[k] != nil {
			t.Errorf("%s got %v, want null", k, empty[k])
		}
	}

	// Ultima pagina: next_cursor null, items [] no nulo.
	h, _ = newTestAPI(t, &fakeStore{})
	_, resp = do(t, h, "GET", "/api/v1/hosts", "", "", asSession)
	if !strings.Contains(string(resp.Data), `"items":[]`) || !strings.Contains(string(resp.Data), `"next_cursor":null`) || !strings.Contains(string(resp.Data), `"total":0`) {
		t.Errorf("vacio got %s", resp.Data)
	}
}

func TestListHostsCursorRoundTripAndTampering(t *testing.T) {
	t.Parallel()
	next := store.HostCursor{NullFlag: 1, SortKey: "40.5", LName: "srv-01", ID: hostUUID}
	fs := &fakeStore{x: fakeExtra{page: store.HostPage{Items: []store.HostRow{{ID: hostUUID, Hostname: "srv-01", Status: "up"}}, Total: 9, NextCursor: &next}}}
	h, _ := newTestAPI(t, fs)

	cursorOf := func(query string) string {
		t.Helper()
		_, resp := do(t, h, "GET", "/api/v1/hosts?"+query, "", "", asSession)
		var d struct {
			NextCursor string `json:"next_cursor"`
		}
		if err := json.Unmarshal(resp.Data, &d); err != nil || d.NextCursor == "" {
			t.Fatalf("sin cursor: %s (%v)", resp.Data, err)
		}
		return d.NextCursor
	}
	c := cursorOf("sort=cpu&status=up&limit=10")

	// Ida y vuelta: el cursor llega al store con la misma posicion.
	rec, _ := do(t, h, "GET", "/api/v1/hosts?sort=cpu&status=up&limit=25&cursor="+url.QueryEscape(c), "", "", asSession)
	if rec.Code != 200 {
		t.Fatalf("cursor valido got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := fs.x.gotFilter.After; got == nil || *got != next {
		t.Errorf("posicion al store got %+v, want %+v (el limit puede cambiar entre paginas)", got, next)
	}

	body, sig, _ := strings.Cut(c, ".")
	forged, _ := json.Marshal(cursorPayload{F: "x", NF: 0, SK: "1; DROP TABLE hosts", LN: "a", ID: hostUUID})
	tests := []struct {
		name, query, cursor string
	}{
		{"otra consulta (otro orden)", "sort=mem&status=up", c},
		{"otro filtro", "sort=cpu&status=down", c},
		{"payload alterado", "sort=cpu&status=up", base64.RawURLEncoding.EncodeToString([]byte(`{"f":"x"}`)) + "." + sig},
		{"firma alterada", "sort=cpu&status=up", body + "." + base64.RawURLEncoding.EncodeToString([]byte("firma-falsa"))},
		{"payload fabricado sin firma valida", "sort=cpu&status=up", base64.RawURLEncoding.EncodeToString(forged) + "." + sig},
		{"sin punto", "sort=cpu&status=up", body},
		{"base64 invalido", "sort=cpu&status=up", "!!!." + sig},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, resp := do(t, h, "GET", "/api/v1/hosts?"+tt.query+"&cursor="+url.QueryEscape(tt.cursor), "", "", asSession)
			wantError(t, rec, resp, 400, "invalid_cursor")
		})
	}

	// Otra instancia de la API (otra clave) no acepta cursores ajenos.
	h2, _ := newTestAPI(t, fs)
	rec, resp := do(t, h2, "GET", "/api/v1/hosts?sort=cpu&status=up&cursor="+url.QueryEscape(c), "", "", asSession)
	wantError(t, rec, resp, 400, "invalid_cursor")
}

func TestListHostsStoreError(t *testing.T) {
	t.Parallel()
	h, logs := newTestAPI(t, &fakeStore{x: fakeExtra{err: errors.New("boom")}})
	rec, resp := do(t, h, "GET", "/api/v1/hosts", "", "", asSession)
	wantError(t, rec, resp, 500, "internal_error")
	if !strings.Contains(logs.String(), "boom") || strings.Contains(rec.Body.String(), "boom") {
		t.Errorf("el 500 debe loguear el error real y no filtrarlo")
	}
}

func TestSummaryShape(t *testing.T) {
	t.Parallel()
	mount := "/"
	fs := viewerStore(&fakeStore{x: fakeExtra{summary: store.Summary{
		Counts: store.StatusCounts{Up: 5, Down: 1, Offline: 2, Pending: 3, Disabled: 4},
		ByGroup: []store.GroupCounts{
			{Group: &store.GroupRef{ID: groupUUID, Name: "Laptops"}, Counts: store.StatusCounts{Up: 1, Offline: 2}},
			{Counts: store.StatusCounts{Up: 4, Down: 1}},
		},
		TopCPU:       []store.TopHost{{ID: "a", Hostname: "h", Value: 91.26}},
		TopMem:       []store.TopHost{},
		TopDisk:      []store.TopHost{{ID: "b", Hostname: "h2", Value: 97, Mount: &mount}},
		RecentlyDown: []store.RecentDown{{ID: "c", Hostname: "h3", StatusChangedAt: fixedNow, Group: &store.GroupRef{ID: groupUUID, Name: "Laptops"}}},
	}}})
	h, _ := newTestAPI(t, fs)
	rec, resp := do(t, h, "GET", "/api/v1/summary", "", "", asSession)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var d map[string]json.RawMessage
	if err := json.Unmarshal(resp.Data, &d); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"generated_at", "counts", "by_group", "top_cpu", "top_mem", "top_disk", "recently_down"} {
		if _, ok := d[k]; !ok {
			t.Errorf("falta %q", k)
		}
	}
	if string(d["generated_at"]) != `"2026-09-23T12:00:00Z"` {
		t.Errorf("generated_at got %s", d["generated_at"])
	}
	if string(d["counts"]) != `{"disabled":4,"down":1,"offline":2,"pending":3,"up":5}` {
		t.Errorf("counts got %s", d["counts"])
	}
	var groups []map[string]any
	if err := json.Unmarshal(d["by_group"], &groups); err != nil {
		t.Fatal(err)
	}
	wantKeys(t, "by_group", groups[0], "group_id", "name", "total", "up", "down", "offline", "pending", "disabled")
	if groups[0]["group_id"] != groupUUID || groups[0]["total"] != 3.0 || groups[1]["group_id"] != nil || groups[1]["name"] != "Sin grupo" || groups[1]["total"] != 5.0 {
		t.Errorf("by_group got %v", groups)
	}
	if string(d["top_cpu"]) != `[{"id":"a","hostname":"h","value":91.3}]` || string(d["top_mem"]) != `[]` ||
		string(d["top_disk"]) != `[{"id":"b","hostname":"h2","value":97,"mount":"/"}]` {
		t.Errorf("tops got %s %s %s", d["top_cpu"], d["top_mem"], d["top_disk"])
	}
	if !strings.Contains(string(d["recently_down"]), `"status_changed_at":"2026-09-23T12:00:00Z"`) || !strings.Contains(string(d["recently_down"]), `"group":{"id":"`+groupUUID) {
		t.Errorf("recently_down got %s", d["recently_down"])
	}
}

func TestHostDetailNewSections(t *testing.T) {
	t.Parallel()
	boot := fixedNow.Add(-72 * time.Hour)
	model, cores, mem := "Ryzen 7", 8, 16000
	fs := &fakeStore{
		detail: store.HostDetail{
			ID: detailHostID, Hostname: "srv", Status: "up", StatusChangedAt: fixedNow, CreatedAt: fixedNow,
			Group: &store.GroupRef{ID: groupUUID, Name: "Srv"},
			Info: store.HostInfo{BootTime: &boot, CPUModel: &model, CPUCores: &cores, MemTotalMB: &mem,
				IPs: []netip.Addr{netip.MustParseAddr("10.0.0.5"), netip.MustParseAddr("fe80::1")}},
		},
		latest: &store.MetricPoint{TS: fixedNow, CPUPct: fp(1), SwapUsedMB: fp(10.4), SwapTotalMB: fp(2048), Load1: fp(0.456), BatteryPct: fp(87), BatteryCharging: new(bool)},
		x:      fakeExtra{disks: []store.DiskUsage{{Mount: "/", UsedMB: 5, TotalMB: 10, TS: fixedNow}}},
	}
	h, _ := newTestAPI(t, fs)
	rec, resp := do(t, h, "GET", "/api/v1/hosts/"+detailHostID, "", "", asSession)
	if rec.Code != 200 {
		t.Fatalf("status %d (%s)", rec.Code, rec.Body.String())
	}
	var d struct {
		Host   map[string]any `json:"host"`
		Info   map[string]any `json:"info"`
		Latest map[string]any `json:"latest"`
		Disks  []struct {
			Mount   string `json:"mount"`
			UsedMB  int    `json:"used_mb"`
			TotalMB int    `json:"total_mb"`
			TS      string `json:"ts"`
		} `json:"disks"`
	}
	if err := json.Unmarshal(resp.Data, &d); err != nil {
		t.Fatal(err)
	}
	if d.Host["group"].(map[string]any)["name"] != "Srv" {
		t.Errorf("host got %v", d.Host)
	}
	if d.Info["cpu_model"] != "Ryzen 7" || d.Info["cpu_cores"] != 8.0 || d.Info["mem_total_mb"] != 16000.0 || d.Info["os_name"] != nil ||
		d.Info["boot_time"] != "2026-09-20T12:00:00Z" || len(d.Info["ips"].([]any)) != 2 {
		t.Errorf("info got %v", d.Info)
	}
	l := d.Latest
	if l["swap_used_mb"] != 10.0 || l["swap_total_mb"] != 2048.0 || l["load1"] != 0.46 || l["load5"] != nil || l["battery_pct"] != 87.0 || l["battery_charging"] != false {
		t.Errorf("latest got %v", l)
	}
	if len(d.Disks) != 1 || d.Disks[0].Mount != "/" || d.Disks[0].TS != "2026-09-23T12:00:00Z" {
		t.Errorf("disks got %+v", d.Disks)
	}
}

func TestHostFingerprintCoversMetricFilters(t *testing.T) {
	t.Parallel()
	parse := func(q string) string {
		v, err := url.ParseQuery(q)
		if err != nil {
			t.Fatal(err)
		}
		hq, err := parseHostQuery(v)
		if err != nil {
			t.Fatal(err)
		}
		return hq.fingerprint
	}
	base := parse("cpu_gt=50")
	if base != parse("cpu_gt=50&limit=10") {
		t.Errorf("el limite no debe cambiar la huella")
	}
	for _, q := range []string{"", "cpu_gt=51", "mem_gt=50", "disk_gt=50", "battery_lt=50", "seen_before=3600"} {
		if parse(q) == base {
			t.Errorf("%q debe dar otra huella que cpu_gt=50 (un cursor no vale entre filtros distintos)", q)
		}
	}
}

func TestHostFingerprintCoversMultiValueFilters(t *testing.T) {
	t.Parallel()
	parse := func(q string) string {
		v, err := url.ParseQuery(q)
		if err != nil {
			t.Fatal(err)
		}
		hq, err := parseHostQuery(v)
		if err != nil {
			t.Fatal(err)
		}
		return hq.fingerprint
	}
	a, b := uuidN(1), uuidN(2)
	base := parse("group=" + a + "&group=" + b + "&group=none&os=linux&os=windows")
	if base != parse("os=windows&group=none&os=linux&group="+b+"&group="+a) {
		t.Errorf("el orden de los valores no debe cambiar la huella")
	}
	for _, q := range []string{"", "group=" + a + "&group=none&os=linux&os=windows", "group=" + a + "&group=" + b + "&os=linux&os=windows",
		"group=" + a + "&group=" + b + "&group=none&os=linux", "group=" + a + "&group=" + b + "&group=none&os=linux&os=windows&os=darwin"} {
		if parse(q) == base {
			t.Errorf("%q debe dar otra huella (un cursor no vale entre filtros distintos)", q)
		}
	}
}

// tag= (parametro de la funcion de etiquetas eliminada) se ignora como cualquier parametro desconocido: un
// marcador o enlace viejo sigue funcionando y no filtra nada.
func TestHostListIgnoresRemovedTagParam(t *testing.T) {
	t.Parallel()
	with, err := parseHostQuery(url.Values{"tag": {"prod", "x"}})
	if err != nil {
		t.Fatalf("tag= debe ignorarse, got %v", err)
	}
	without, err := parseHostQuery(url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	if with.fingerprint != without.fingerprint {
		t.Error("tag= no debe alterar filtro ni huella del cursor")
	}
}
