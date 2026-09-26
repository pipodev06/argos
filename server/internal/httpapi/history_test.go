package httpapi

import (
	"context"
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"argos/server/internal/chunk"
)

// Integracion de GET /hosts/{id}/history contra Postgres real.

type historyEnv struct {
	*scopeEnv
	day time.Time // dia UTC (hace 2 dias) donde se siembran las muestras crudas
}

func newHistoryEnv(t *testing.T) *historyEnv {
	t.Helper()
	s := newScopeEnv(t)
	h := &historyEnv{scopeEnv: s, day: s.clock.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -2)}
	// 20 muestras de 30 s desde las 10:00; el contador de rx se reinicia en la 10.
	rx, tx := int64(0), int64(0)
	for i := 0; i < 20; i++ {
		rx += 3000
		if i == 10 {
			rx = 500
		}
		tx += 300
		h.exec(`INSERT INTO metrics (host_id, ts, cpu_pct, mem_used_mb, mem_total_mb, net_rx_bytes, net_tx_bytes)
		        VALUES ($1, $2, $3, 100, 200, $4, $5)`, s.hostA, h.day.Add(10*time.Hour+time.Duration(i)*30*time.Second), float64(i), rx, tx)
	}
	return h
}

func (h *historyEnv) exec(sql string, args ...any) {
	h.t.Helper()
	if _, err := testPool.Exec(context.Background(), sql, args...); err != nil {
		h.t.Fatalf("exec %q: %v", sql, err)
	}
}

func (h *historyEnv) get(c *http.Cookie, hostID string, from, to time.Time, extra ...string) *httptest.ResponseRecorder {
	h.t.Helper()
	q := url.Values{"from": {from.UTC().Format(time.RFC3339)}, "to": {to.UTC().Format(time.RFC3339)}}
	for i := 0; i+1 < len(extra); i += 2 {
		q.Set(extra[i], extra[i+1])
	}
	return h.do("GET", "/api/v1/hosts/"+hostID+"/history?"+q.Encode(), "", withCookie(c))
}

type historyResp struct {
	Host struct {
		ID       string `json:"id"`
		Hostname string `json:"hostname"`
	} `json:"host"`
	Resolution          string     `json:"resolution"`
	StepSeconds         int        `json:"step_seconds"`
	AvailableFrom       *time.Time `json:"available_from"`
	DetailRetentionDays int        `json:"detail_retention_days"`
	Points              []struct {
		TS       time.Time `json:"ts"`
		CPUPct   *float64  `json:"cpu_pct"`
		NetRxBps *int64    `json:"net_rx_bps"`
	} `json:"points"`
}

func (h *historyEnv) json(c *http.Cookie, hostID string, from, to time.Time) historyResp {
	h.t.Helper()
	rec := h.get(c, hostID, from, to)
	h.wantStatus(rec, http.StatusOK, "")
	var r historyResp
	h.dataAs(rec, &r)
	return r
}

func (h *historyEnv) csvRows(rec *httptest.ResponseRecorder) [][]string {
	h.t.Helper()
	h.wantStatus(rec, http.StatusOK, "")
	body := strings.TrimPrefix(rec.Body.String(), "\xEF\xBB\xBF")
	rows, err := csv.NewReader(strings.NewReader(body)).ReadAll()
	if err != nil {
		h.t.Fatalf("csv invalido: %v\n%s", err, body)
	}
	return rows
}

func TestHistoryJSONDayInsideData(t *testing.T) {
	h := newHistoryEnv(t)
	from := h.day.Add(10*time.Hour + 2*time.Minute)
	r := h.json(h.admin, h.hostA, from, from.Add(4*time.Minute)) // muestras 4..11
	if r.Resolution != "raw" || r.StepSeconds != 30 || r.DetailRetentionDays != 30 || r.Host.ID != h.hostA || r.Host.Hostname != "host-a" {
		t.Errorf("cabecera got %+v", r)
	}
	if len(r.Points) != 8 || r.Points[0].CPUPct == nil || *r.Points[0].CPUPct != 4 || *r.Points[7].CPUPct != 11 {
		t.Fatalf("puntos got %+v, want 8 con cpu 4..11", r.Points)
	}
	// El tramo de la muestra 10 reinicia el contador: sin tasa.
	if r.Points[6].NetRxBps != nil || r.Points[5].NetRxBps == nil || *r.Points[5].NetRxBps != 100 {
		t.Errorf("tasas got %+v, want 100 B/s y nil en el reinicio", r.Points)
	}
	if r.AvailableFrom == nil || !r.AvailableFrom.Equal(from) {
		t.Errorf("available_from got %v, want %s", r.AvailableFrom, from)
	}
	// Un rango sin datos no es error: vacio y sin available_from.
	r = h.json(h.admin, h.hostA, h.day.Add(3*time.Hour), h.day.Add(4*time.Hour))
	if len(r.Points) != 0 || r.AvailableFrom != nil {
		t.Errorf("sin datos got %+v", r)
	}
}

// El detalle de un dia que ya esta empaquetado (chunk) se sirve igual, sin filas crudas.
func TestHistoryJSONReadsPackedDay(t *testing.T) {
	h := newHistoryEnv(t)
	day := h.day.AddDate(0, 0, -10)
	var rows []chunk.Row
	for i := 0; i < 2880; i++ {
		cpu, mu, mt := float32(i%10), int32(50), int32(100)
		rows = append(rows, chunk.Row{TS: day.Add(time.Duration(i) * 30 * time.Second), CPUPct: &cpu, MemUsedMB: &mu, MemTotalMB: &mt})
	}
	blob, err := chunk.Encode(rows)
	if err != nil {
		t.Fatal(err)
	}
	h.exec(`INSERT INTO metrics_chunks (host_id, day, n, codec, data) VALUES ($1, $2::date, $3, $4, $5)`, h.hostA, day, len(rows), chunk.Version, blob)

	r := h.json(h.admin, h.hostA, day.Add(6*time.Hour), day.Add(7*time.Hour))
	if r.Resolution != "raw" || len(r.Points) != 120 || *r.Points[0].CPUPct != 0 || *r.Points[9].CPUPct != 9 {
		t.Errorf("got %s con %d puntos, want raw con 120", r.Resolution, len(r.Points))
	}
}

func TestHistoryResolutionFollowsEffectiveRetention(t *testing.T) {
	h := newHistoryEnv(t)
	now := h.clock.Now()
	ctx := context.Background()
	span := 24 * time.Hour
	at := func(daysAgo int) historyResp {
		return h.json(h.admin, h.hostA, now.AddDate(0, 0, -daysAgo), now.AddDate(0, 0, -daysAgo).Add(span))
	}

	if r := at(20); r.Resolution != "raw" || r.DetailRetentionDays != 30 {
		t.Errorf("20 d con retencion 30: got %s/%d, want raw/30", r.Resolution, r.DetailRetentionDays)
	}
	if r := at(40); r.Resolution != "hourly" || r.StepSeconds != 3600 {
		t.Errorf("40 d con retencion 30: got %s/%d s, want hourly/3600", r.Resolution, r.StepSeconds)
	}
	days := 90
	if err := h.st.SetHostDetailDays(ctx, h.hostA, &days); err != nil {
		t.Fatal(err)
	}
	if r := at(40); r.Resolution != "raw" || r.DetailRetentionDays != 90 {
		t.Errorf("40 d con retencion 90: got %s/%d, want raw/90", r.Resolution, r.DetailRetentionDays)
	}
	if r := at(100); r.Resolution != "hourly" {
		t.Errorf("100 d con retencion 90: got %s, want hourly", r.Resolution)
	}
}

func TestHistoryValidationAndCaps(t *testing.T) {
	h := newHistoryEnv(t)
	now := h.clock.Now()
	days := 90
	if err := h.st.SetHostDetailDays(context.Background(), h.hostA, &days); err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/hosts/" + h.hostA + "/history"
	iso := func(tm time.Time) string { return url.QueryEscape(tm.UTC().Format(time.RFC3339)) }
	tests := []struct {
		name, query, code string
		status            int
	}{
		{"sin from", "to=" + iso(now), "invalid_range", 400},
		{"sin to", "from=" + iso(now.Add(-time.Hour)), "invalid_range", 400},
		{"from no es fecha", "from=ayer&to=" + iso(now), "invalid_range", 400},
		{"to <= from", "from=" + iso(now) + "&to=" + iso(now.Add(-time.Hour)), "invalid_range", 400},
		{"to en el futuro", "from=" + iso(now.Add(-time.Hour)) + "&to=" + iso(now.Add(time.Hour)), "invalid_range", 400},
		{"from absurdo", "from=1970-01-01T00:00:00Z&to=1970-01-02T00:00:00Z", "invalid_range", 400},
		{"format desconocido", "from=" + iso(now.Add(-time.Hour)) + "&to=" + iso(now) + "&format=xml", "invalid_body", 400},
		{"series desconocida", "from=" + iso(now.Add(-time.Hour)) + "&to=" + iso(now) + "&series=x", "invalid_body", 400},
		{"disks solo csv", "from=" + iso(now.Add(-time.Hour)) + "&to=" + iso(now) + "&series=disks", "invalid_body", 400},
		{"raw pasa de 31 d", "from=" + iso(now.AddDate(0, 0, -40)) + "&to=" + iso(now), "range_too_large", 400},
		{"raw justo 31 d", "from=" + iso(now.AddDate(0, 0, -31)) + "&to=" + iso(now), "", 200},
		{"hourly pasa de 366 d", "from=" + iso(now.AddDate(0, 0, -399)) + "&to=" + iso(now), "range_too_large", 400},
		{"hourly 366 d", "from=" + iso(now.AddDate(0, 0, -366)) + "&to=" + iso(now.Add(-time.Hour)), "", 200},
		{"disks pasa de 31 d", "from=" + iso(now.AddDate(0, 0, -40)) + "&to=" + iso(now) + "&format=csv&series=disks", "range_too_large", 400},
	}
	for _, tt := range tests {
		rec := h.do("GET", base+"?"+tt.query, "", withCookie(h.admin))
		h.wantStatus(rec, tt.status, tt.code)
	}
	// Sin sesion.
	h.wantStatus(h.do("GET", base+"?from=x", ""), http.StatusUnauthorized, "unauthenticated")
	h.wantStatus(h.do("GET", "/api/v1/hosts/no-uuid/history?from=x", "", withCookie(h.admin)), http.StatusBadRequest, "invalid_id")
}

// Un viewer restringido recibe el mismo 404 que un host inexistente, tambien con CSV y discos.
func TestHistoryScope(t *testing.T) {
	h := newHistoryEnv(t)
	now := h.clock.Now()
	from, to := now.Add(-time.Hour), now
	missing := h.get(h.va, "00000000-0000-4000-8000-000000000000", from, to)
	h.wantStatus(missing, http.StatusNotFound, "not_found")
	for _, extra := range [][]string{nil, {"format", "csv"}, {"format", "csv", "series", "disks"}} {
		got := h.get(h.va, h.hostB, from, to, extra...)
		h.wantStatus(got, http.StatusNotFound, "not_found")
		if got.Body.String() != missing.Body.String() {
			t.Errorf("%v: cuerpo distinto del de un host inexistente:\n%s\n%s", extra, got.Body.String(), missing.Body.String())
		}
	}
	h.wantStatus(h.get(h.va, h.hostA, from, to), http.StatusOK, "") // su propio host si
}

// deadlineRecorder registra el SetWriteDeadline que pide el handler (httptest no lo soporta).
type deadlineRecorder struct {
	*httptest.ResponseRecorder
	deadline time.Time
}

func (d *deadlineRecorder) SetWriteDeadline(t time.Time) error { d.deadline = t; return nil }

func TestHistoryCSVRaw(t *testing.T) {
	h := newHistoryEnv(t)
	// Hostname con caracteres peligrosos: el archivo sale saneado.
	h.exec(`UPDATE hosts SET hostname = $2 WHERE id = $1`, h.hostA, `a b/../"x";=y`)
	from := h.day.Add(10 * time.Hour)
	q := url.Values{"from": {from.Format(time.RFC3339)}, "to": {from.Add(10 * time.Minute).Format(time.RFC3339)}, "format": {"csv"}}
	req := httptest.NewRequest("GET", "/api/v1/hosts/"+h.hostA+"/history?"+q.Encode(), nil)
	req.Host = "localhost:8080"
	req.AddCookie(h.admin)
	rec := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	h.h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="metricas-a_b_..__x___y-`+from.Format(time.DateOnly)+`_`+from.Format(time.DateOnly)+`.csv"` {
		t.Errorf("Content-Disposition got %q", got)
	}
	if left := time.Until(rec.deadline); left < 2*time.Minute {
		t.Errorf("SetWriteDeadline con %s de margen, want el plazo largo de exportacion", left)
	}
	rows := h.csvRows(rec.ResponseRecorder)
	if len(rows) != 21 { // cabecera + 20 muestras (10 min)
		t.Fatalf("filas got %d, want 21", len(rows))
	}
	head := rows[0]
	if head[0] != "ts" || head[len(head)-1] != "net_tx_bps" || len(head) != 15 {
		t.Errorf("cabecera got %v", head)
	}
	col := func(name string) int {
		for i, c := range head {
			if c == name {
				return i
			}
		}
		t.Fatalf("falta la columna %s", name)
		return -1
	}
	// Fila 1 = muestra 0 (sin tasa); fila 3 = muestra 2 (100 B/s); muestra 10 reinicia el contador (fila 11).
	if rows[1][col("net_rx_bps")] != "" || rows[3][col("net_rx_bps")] != "100" || rows[3][col("net_tx_bps")] != "10" {
		t.Errorf("tasas fila1 %v fila3 %v", rows[1], rows[3])
	}
	if rows[11][col("net_rx_bytes")] != "500" || rows[11][col("net_rx_bps")] != "" {
		t.Errorf("reinicio de contador: fila %v, want net_rx_bps vacio", rows[11])
	}
	if rows[1][0] != from.Format(time.RFC3339) || rows[3][col("cpu_pct")] != "2" || rows[3][col("mem_total_mb")] != "200" {
		t.Errorf("fila 3 got %v", rows[3])
	}

	// Auditoria: un evento con host, rango y resolucion; sin datos.
	var detail string
	if err := testPool.QueryRow(context.Background(), `SELECT detail::text FROM audit_log WHERE event = 'history_export' AND target_id = $1`, h.hostA).Scan(&detail); err != nil {
		t.Fatalf("auditoria history_export: %v", err)
	}
	for _, want := range []string{`"resolution": "raw"`, `"series": "metrics"`, `"rows": 20`} {
		if !strings.Contains(detail, want) {
			t.Errorf("detalle de auditoria %s sin %s", detail, want)
		}
	}
	// El JSON no audita.
	h.json(h.admin, h.hostA, from, from.Add(time.Minute))
	if n := h.count(`SELECT count(*) FROM audit_log WHERE event = 'history_export'`); n != 1 {
		t.Errorf("eventos history_export got %d, want 1 (solo el CSV)", n)
	}
}

func TestHistoryCSVHourlyAndDisks(t *testing.T) {
	h := newHistoryEnv(t)
	now := h.clock.Now().UTC()
	old := now.Truncate(time.Hour).AddDate(0, 0, -40)
	for i := 0; i < 3; i++ {
		h.exec(`INSERT INTO metrics_hourly (host_id, hour, samples, cpu_avg, cpu_min, cpu_max, mem_used_avg_mb, mem_total_mb)
		        VALUES ($1, $2, 120, 12.5, 1, 90, 100, 200)`, h.hostA, old.Add(time.Duration(i)*time.Hour))
	}
	rows := h.csvRows(h.get(h.admin, h.hostA, old, old.Add(3*time.Hour), "format", "csv"))
	if len(rows) != 4 || rows[0][0] != "hour" || rows[0][1] != "samples" || rows[0][2] != "cpu_avg" {
		t.Fatalf("horario got %v", rows)
	}
	if rows[1][0] != old.Format(time.RFC3339) || rows[1][1] != "120" || rows[1][2] != "12.5" || rows[1][4] != "90" || rows[1][8] != "" {
		t.Errorf("fila horaria got %v", rows[1])
	}

	// Discos: el punto de montaje viene del agente y se neutraliza (=, +, -, @).
	ts := h.day.Add(10 * time.Hour)
	for _, m := range []string{"/data", "=cmd|' /C calc'!A0", "+1", "@x"} {
		h.exec(`INSERT INTO disk_samples (ts, host_id, used_mb, total_mb, mount) VALUES ($1, $2, 25, 100, $3)`, ts, h.hostA, m)
	}
	rows = h.csvRows(h.get(h.admin, h.hostA, ts, ts.Add(time.Hour), "format", "csv", "series", "disks"))
	if len(rows) != 5 || strings.Join(rows[0], ",") != "ts,mount,used_mb,total_mb,used_pct" {
		t.Fatalf("discos got %v", rows)
	}
	for _, r := range rows[1:] {
		if r[4] != "25" || strings.ContainsAny(r[1][:1], "=+-@") {
			t.Errorf("fila de disco %v: used_pct 25 y montaje neutralizado", r)
		}
	}
	// Un rango mas viejo que lo retenido devuelve solo la cabecera.
	rows = h.csvRows(h.get(h.admin, h.hostA, now.AddDate(0, 0, -20), now.AddDate(0, 0, -19), "format", "csv", "series", "disks"))
	if len(rows) != 1 {
		t.Errorf("discos sin datos got %d filas, want solo cabecera", len(rows))
	}
}

func TestHistoryCSVRateLimitAndQuota(t *testing.T) {
	h := newHistoryEnv(t)
	now := h.clock.Now()
	for i := 0; i < exportsPerMinute; i++ {
		h.wantStatus(h.get(h.admin, h.hostA, now.Add(-time.Hour), now, "format", "csv"), http.StatusOK, "")
	}
	rec := h.get(h.admin, h.hostA, now.Add(-time.Hour), now, "format", "csv")
	h.wantStatus(rec, http.StatusTooManyRequests, "rate_limited")
	if rec.Header().Get("Retry-After") == "" {
		t.Error("el 429 debe llevar Retry-After")
	}
	// El JSON no cuenta para el limite de exportaciones.
	h.wantStatus(h.get(h.admin, h.hostA, now.Add(-time.Hour), now), http.StatusOK, "")
	// Otro usuario tiene su propio limite.
	h.wantStatus(h.get(h.va, h.hostA, now.Add(-time.Hour), now, "format", "csv"), http.StatusOK, "")
}

func TestExportGate(t *testing.T) {
	t.Parallel()
	g := newExportGate(2)
	relA, err := g.acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.acquire("a"); err == nil {
		t.Error("una segunda exportacion concurrente del mismo usuario debe rechazarse")
	}
	relB, err := g.acquire("b")
	if err != nil {
		t.Fatalf("otro usuario debe poder: %v", err)
	}
	if _, err := g.acquire("c"); err == nil {
		t.Error("el tope global de 2 debe rechazar a un tercer usuario")
	}
	relA()
	relC, err := g.acquire("c")
	if err != nil {
		t.Fatalf("tras liberar debe haber cupo: %v", err)
	}
	relA() // liberar dos veces no debe romper el conteo del cupo de otros: se libera solo lo propio
	relB()
	relC()
}
