package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"argos/server/internal/store"
)

const detailHostID = "0f8fad5b-d9cb-469f-a165-70867728950e"

func (f *fakeStore) HostByID(_ context.Context, _ store.Scope, _ string) (store.HostDetail, error) {
	return f.detail, f.detailErr
}

func (f *fakeStore) LatestMetrics(_ context.Context, _ string) (*store.MetricPoint, error) {
	return f.latest, f.latestErr
}

func (f *fakeStore) MetricSeries(_ context.Context, _ string, from time.Time, step time.Duration) ([]store.MetricPoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gotFrom, f.gotStep, f.gotHourly = from, step, false
	return f.series, f.seriesErr
}

func (f *fakeStore) MetricSeriesHourly(_ context.Context, _ string, from time.Time, step time.Duration) ([]store.MetricPoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gotFrom, f.gotStep, f.gotHourly = from, step, true
	return f.series, f.seriesErr
}

func fp(v float64) *float64 { return &v }

func TestHostDetailAccessAndErrors(t *testing.T) {
	t.Parallel()

	const detail = "/api/v1/hosts/" + detailHostID
	tests := []struct {
		name       string
		path       string
		session    bool
		viewer     bool
		detailErr  error
		latestErr  error
		seriesErr  error
		wantStatus int
		wantCode   string
	}{
		{name: "detalle sin sesion", path: detail, wantStatus: 401, wantCode: "unauthenticated"},
		{name: "serie sin sesion", path: detail + "/metrics", wantStatus: 401, wantCode: "unauthenticated"},
		{name: "detalle admin", path: detail, session: true, wantStatus: 200},
		{name: "detalle viewer", path: detail, session: true, viewer: true, wantStatus: 200},
		{name: "serie admin", path: detail + "/metrics", session: true, wantStatus: 200},
		{name: "serie viewer", path: detail + "/metrics", session: true, viewer: true, wantStatus: 200},
		{name: "detalle id invalido", path: "/api/v1/hosts/no-es-uuid", session: true, wantStatus: 400, wantCode: "invalid_id"},
		{name: "serie id invalido", path: "/api/v1/hosts/no-es-uuid/metrics", session: true, wantStatus: 400, wantCode: "invalid_id"},
		{name: "detalle 404", path: detail, session: true, detailErr: store.ErrNotFound, wantStatus: 404, wantCode: "not_found"},
		{name: "serie 404", path: detail + "/metrics", session: true, seriesErr: store.ErrNotFound, wantStatus: 404, wantCode: "not_found"},
		{name: "rango invalido", path: detail + "/metrics?range=2h", session: true, wantStatus: 400, wantCode: "invalid_range"},
		{name: "rango con mayusculas", path: detail + "/metrics?range=1H", session: true, wantStatus: 400, wantCode: "invalid_range"},
		{name: "rango largo no permitido", path: detail + "/metrics?range=2y", session: true, wantStatus: 400, wantCode: "invalid_range"},
		{name: "rango largo con mayusculas", path: detail + "/metrics?range=30D", session: true, wantStatus: 400, wantCode: "invalid_range"},
		{name: "rango largo 30d", path: detail + "/metrics?range=30d", session: true, wantStatus: 200},
		{name: "rango largo 1y", path: detail + "/metrics?range=1y", session: true, wantStatus: 200},
		{name: "serie horaria 404", path: detail + "/metrics?range=90d", session: true, seriesErr: store.ErrNotFound, wantStatus: 404, wantCode: "not_found"},
		{name: "error de db en serie horaria es 500", path: detail + "/metrics?range=90d", session: true, seriesErr: errors.New("boom"), wantStatus: 500, wantCode: "internal_error"},
		{name: "rango vacio usa el default", path: detail + "/metrics?range=", session: true, wantStatus: 200},
		{name: "error de db en detalle es 500", path: detail, session: true, detailErr: errors.New("boom"), wantStatus: 500, wantCode: "internal_error"},
		{name: "error de db en latest es 500", path: detail, session: true, latestErr: errors.New("boom"), wantStatus: 500, wantCode: "internal_error"},
		{name: "error de db en serie es 500", path: detail + "/metrics", session: true, seriesErr: errors.New("boom"), wantStatus: 500, wantCode: "internal_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := &fakeStore{detailErr: tt.detailErr, latestErr: tt.latestErr, seriesErr: tt.seriesErr}
			if tt.viewer {
				fs.sessionUser = store.User{ID: "u-viewer", Username: "viewer", Role: store.RoleViewer}
			}
			h, logs := newTestAPI(t, fs)
			var mods []func(*http.Request)
			if tt.session {
				mods = append(mods, asSession)
			}
			rec, resp := do(t, h, "GET", tt.path, "", "", mods...)
			if tt.wantCode != "" {
				wantError(t, rec, resp, tt.wantStatus, tt.wantCode)
			} else if rec.Code != tt.wantStatus {
				t.Fatalf("status got %d, want %d (body %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantStatus == 500 {
				if !strings.Contains(logs.String(), "boom") {
					t.Errorf("el 500 no logueo el error real: %q", logs.String())
				}
				if strings.Contains(rec.Body.String(), "boom") {
					t.Errorf("el error real se filtro al cliente: %s", rec.Body.String())
				}
			}
		})
	}
}

func wantKeys(t *testing.T, name string, m map[string]any, keys ...string) {
	t.Helper()
	if len(m) != len(keys) {
		t.Errorf("%s tiene %d claves (%v), want exactamente %d", name, len(m), m, len(keys))
	}
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			t.Errorf("falta la clave %q en %s", k, name)
		}
	}
}

// Claves de un punto de serie (las originales + swap, carga y bateria) y de latest (el punto + battery_charging).
var (
	pointKeys  = []string{"ts", "cpu_pct", "mem_used_mb", "mem_total_mb", "net_rx_bps", "net_tx_bps", "swap_used_mb", "swap_total_mb", "load1", "load5", "load15", "battery_pct"}
	latestKeys = append(slices.Clone(pointKeys), "battery_charging")
)

func TestHostDetailShape(t *testing.T) {
	t.Parallel()

	seen := fixedNow.Add(-time.Minute)
	detail := store.HostDetail{
		ID: detailHostID, Hostname: "srv-01", Kind: "agent", OS: "linux", Arch: "amd64", Status: "up",
		LastSeenAt: &seen, StatusChangedAt: fixedNow.Add(-2 * time.Hour), CreatedAt: fixedNow.Add(-24 * time.Hour),
	}

	t.Run("con metricas", func(t *testing.T) {
		t.Parallel()
		fs := &fakeStore{detail: detail, latest: &store.MetricPoint{
			TS: fixedNow, CPUPct: fp(12.34), MemUsedMB: fp(100.4), MemTotalMB: fp(200), NetRxBps: fp(1234.6), NetTxBps: fp(0),
		}}
		h, _ := newTestAPI(t, fs)
		rec, resp := do(t, h, "GET", "/api/v1/hosts/"+detailHostID, "", "", asSession)
		if rec.Code != 200 || resp.Error != nil {
			t.Fatalf("status got %d error %+v", rec.Code, resp.Error)
		}
		var raw map[string]any
		if err := json.Unmarshal(resp.Data, &raw); err != nil {
			t.Fatal(err)
		}
		wantKeys(t, "data", raw, "host", "info", "latest", "disks")
		data := map[string]map[string]any{"host": raw["host"].(map[string]any), "latest": raw["latest"].(map[string]any), "info": raw["info"].(map[string]any)}
		wantKeys(t, "info", data["info"], "boot_time", "cpu_model", "cpu_cores", "cpu_threads", "os_name", "os_version", "kernel_version", "agent_version", "privilege", "ips", "mem_total_mb", "physical_disks")
		wantKeys(t, "host", data["host"], "id", "hostname", "kind", "os", "arch", "status", "last_seen_at", "status_changed_at", "created_at", "group")
		wantKeys(t, "latest", data["latest"], latestKeys...)
		if got := data["host"]["status_changed_at"]; got != "2026-09-23T10:00:00Z" {
			t.Errorf("status_changed_at got %v, want ISO 8601 UTC 2026-09-23T10:00:00Z", got)
		}
		l := data["latest"]
		if l["cpu_pct"] != 12.3 || l["mem_used_mb"] != 100.0 || l["net_rx_bps"] != 1235.0 || l["net_tx_bps"] != 0.0 {
			t.Errorf("latest got %v, want cpu 12.3, mem 100, rx 1235, tx 0", l)
		}
		if l["ts"] != "2026-09-23T12:00:00Z" {
			t.Errorf("ts got %v, want ISO 8601 UTC", l["ts"])
		}
	})
	t.Run("sin metricas latest es null", func(t *testing.T) {
		t.Parallel()
		h, _ := newTestAPI(t, &fakeStore{detail: detail})
		_, resp := do(t, h, "GET", "/api/v1/hosts/"+detailHostID, "", "", asSession)
		var data map[string]json.RawMessage
		if err := json.Unmarshal(resp.Data, &data); err != nil {
			t.Fatal(err)
		}
		if got, ok := data["latest"]; !ok || string(got) != "null" {
			t.Errorf("latest got %s (presente=%v), want null", got, ok)
		}
	})
	t.Run("net nulo se serializa null", func(t *testing.T) {
		t.Parallel()
		fs := &fakeStore{detail: detail, latest: &store.MetricPoint{TS: fixedNow, CPUPct: fp(1)}}
		h, _ := newTestAPI(t, fs)
		_, resp := do(t, h, "GET", "/api/v1/hosts/"+detailHostID, "", "", asSession)
		var data struct {
			Latest map[string]any `json:"latest"`
		}
		if err := json.Unmarshal(resp.Data, &data); err != nil {
			t.Fatal(err)
		}
		wantKeys(t, "latest", data.Latest, latestKeys...)
		if data.Latest["net_rx_bps"] != nil || data.Latest["net_tx_bps"] != nil {
			t.Errorf("net got %v, want null", data.Latest)
		}
	})
}

func TestHostMetricsRangesAndShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		query    string
		wantName string
		wantStep int
		wantFrom time.Duration
		hourly   bool // lee metrics_hourly en vez del crudo
	}{
		{"", "1h", 30, time.Hour, false},
		{"?range=1h", "1h", 30, time.Hour, false},
		{"?range=6h", "6h", 120, 6 * time.Hour, false},
		{"?range=24h", "24h", 600, 24 * time.Hour, false},
		{"?range=7d", "7d", 3600, 7 * 24 * time.Hour, false},
		{"?range=30d", "30d", 3600, 30 * 24 * time.Hour, true},
		{"?range=90d", "90d", 10800, 90 * 24 * time.Hour, true},
		{"?range=1y", "1y", 43200, 365 * 24 * time.Hour, true},
	}
	for _, tt := range tests {
		t.Run("range"+tt.query, func(t *testing.T) {
			t.Parallel()
			fs := &fakeStore{series: []store.MetricPoint{
				{TS: fixedNow.Add(-time.Minute), CPUPct: fp(50.06), MemUsedMB: fp(10), MemTotalMB: fp(20), NetRxBps: fp(99.5)},
				{TS: fixedNow, CPUPct: fp(1)},
			}}
			h, _ := newTestAPI(t, fs)
			rec, resp := do(t, h, "GET", "/api/v1/hosts/"+detailHostID+"/metrics"+tt.query, "", "", asSession)
			if rec.Code != 200 {
				t.Fatalf("status got %d (%s)", rec.Code, rec.Body.String())
			}
			var data struct {
				Range       string           `json:"range"`
				StepSeconds int              `json:"step_seconds"`
				Points      []map[string]any `json:"points"`
			}
			if err := json.Unmarshal(resp.Data, &data); err != nil {
				t.Fatal(err)
			}
			if data.Range != tt.wantName || data.StepSeconds != tt.wantStep {
				t.Errorf("range/step got %q/%d, want %q/%d", data.Range, data.StepSeconds, tt.wantName, tt.wantStep)
			}
			if want := fixedNow.Add(-tt.wantFrom); !fs.gotFrom.Equal(want) {
				t.Errorf("from got %v, want %v", fs.gotFrom, want)
			}
			if fs.gotStep != time.Duration(tt.wantStep)*time.Second {
				t.Errorf("step al store got %v, want %ds", fs.gotStep, tt.wantStep)
			}
			if len(data.Points) != 2 {
				t.Fatalf("points got %d, want 2", len(data.Points))
			}
			wantKeys(t, "point", data.Points[0], pointKeys...)
			p := data.Points[0]
			if p["cpu_pct"] != 50.1 || p["net_rx_bps"] != 100.0 || p["net_tx_bps"] != nil {
				t.Errorf("punto got %v, want cpu 50.1, rx 100, tx null", p)
			}
			if p["ts"] != "2026-09-23T11:59:00Z" {
				t.Errorf("ts got %v, want ISO 8601 UTC", p["ts"])
			}
		})
	}

	t.Run("sin puntos es lista vacia no nula", func(t *testing.T) {
		t.Parallel()
		h, _ := newTestAPI(t, &fakeStore{})
		rec, _ := do(t, h, "GET", "/api/v1/hosts/"+detailHostID+"/metrics", "", "", asSession)
		if !strings.Contains(rec.Body.String(), `"points":[]`) {
			t.Errorf("body got %s, want points:[]", rec.Body.String())
		}
	})
}

// Cada rango de la allowlist devuelve a lo sumo duration/step + 1 buckets: la respuesta esta acotada.
func TestMetricRangesBoundPoints(t *testing.T) {
	t.Parallel()
	for name, r := range metricRanges {
		if max := int(r.duration/r.step) + 1; max > 1000 {
			t.Errorf("rango %s: hasta %d puntos, want <= 1000", name, max)
		}
		if r.hourly && r.step%time.Hour != 0 {
			t.Errorf("rango %s: step %v no es multiplo de 1 h (los resumenes son por hora)", name, r.step)
		}
	}
}

func TestInvalidRangeMessageListsAllRanges(t *testing.T) {
	t.Parallel()
	h, _ := newTestAPI(t, &fakeStore{})
	rec, resp := do(t, h, "GET", "/api/v1/hosts/"+detailHostID+"/metrics?range=2h", "", "", asSession)
	wantError(t, rec, resp, 400, "invalid_range")
	for name := range metricRanges {
		if !strings.Contains(resp.Error.Message, name) {
			t.Errorf("el mensaje %q no menciona el rango %s", resp.Error.Message, name)
		}
	}
}

// physical_disks: null si el agente no lo reporta (nunca []); con datos, campos ausentes como null.
func TestHostDetailPhysicalDisks(t *testing.T) {
	t.Parallel()
	detail := store.HostDetail{ID: detailHostID, Hostname: "srv-01", Kind: "agent", Status: "up", StatusChangedAt: fixedNow, CreatedAt: fixedNow}
	get := func(t *testing.T, disks []store.PhysicalDisk) any {
		t.Helper()
		d := detail
		d.Info.PhysicalDisks = disks
		h, _ := newTestAPI(t, &fakeStore{detail: d})
		rec, resp := do(t, h, "GET", "/api/v1/hosts/"+detailHostID, "", "", asSession)
		if rec.Code != 200 {
			t.Fatalf("status got %d, want 200", rec.Code)
		}
		var raw struct {
			Info map[string]any `json:"info"`
		}
		if err := json.Unmarshal(resp.Data, &raw); err != nil {
			t.Fatal(err)
		}
		return raw.Info["physical_disks"]
	}
	t.Run("desconocido es null", func(t *testing.T) {
		t.Parallel()
		if got := get(t, nil); got != nil {
			t.Errorf("physical_disks got %v, want null", got)
		}
	})
	t.Run("con datos", func(t *testing.T) {
		t.Parallel()
		model, size := "SAMSUNG", int64(488386)
		got, ok := get(t, []store.PhysicalDisk{{ID: "0", Model: &model, SizeMB: &size, Mounts: []string{"D:\\"}}, {ID: "1"}}).([]any)
		if !ok || len(got) != 2 {
			t.Fatalf("physical_disks got %v, want 2 discos", got)
		}
		wantKeys(t, "disco 0", got[0].(map[string]any), "id", "model", "bus", "size_mb", "mounts")
		second := got[1].(map[string]any)
		if second["model"] != nil || second["bus"] != nil || second["size_mb"] != nil {
			t.Errorf("campos ausentes got %v, want null", second)
		}
		if m, ok := second["mounts"].([]any); !ok || len(m) != 0 {
			t.Errorf("mounts got %v, want []", second["mounts"])
		}
	})
}
