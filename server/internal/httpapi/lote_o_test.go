package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"argos/server/internal/agentdist"
	"argos/server/internal/beat"
	"argos/server/internal/store"
)

// --- H5: tope de frecuencia de ingest ---

func TestIngestGate(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	at := func(s int) time.Time { return base.Add(time.Duration(s) * time.Second) }

	t.Run("espaciado normal pasa", func(t *testing.T) {
		t.Parallel()
		var g ingestGate
		for _, s := range []int{0, 5, 10, 40, 70} { // 5 s es el minimo del agente
			if g.tooSoon("h", at(s)) {
				t.Fatalf("ts +%ds se descarto, want aceptado", s)
			}
			g.accepted("h", at(s))
		}
	})
	t.Run("rafaga se recorta", func(t *testing.T) {
		t.Parallel()
		var g ingestGate
		g.accepted("h", at(0))
		accepted := 0
		for s := 1; s <= 12; s++ { // una muestra por segundo
			if !g.tooSoon("h", at(s)) {
				g.accepted("h", at(s))
				accepted++
			}
		}
		if accepted != 3 { // +4, +8, +12
			t.Errorf("aceptadas got %d, want 3 en 12 s", accepted)
		}
	})
	t.Run("replay historico del buffer pasa", func(t *testing.T) {
		t.Parallel()
		var g ingestGate
		g.accepted("h", at(600)) // la muestra en vivo llego primero
		for s := 0; s < 600; s += 30 {
			if g.tooSoon("h", at(s)) {
				t.Fatalf("replay +%ds se descarto, want aceptado", s)
			}
			g.accepted("h", at(s))
		}
		if !g.tooSoon("h", at(601)) {
			t.Error("tras el replay, la maxima sigue siendo +600: +601 debe descartarse")
		}
	})
	t.Run("hosts distintos no se afectan", func(t *testing.T) {
		t.Parallel()
		var g ingestGate
		g.accepted("a", at(0))
		if g.tooSoon("b", at(0)) {
			t.Error("el host b se descarto por una muestra del host a")
		}
		if !g.tooSoon("a", at(1)) {
			t.Error("el host a debia descartar +1 s")
		}
	})
	t.Run("el limite es estricto a 4 s", func(t *testing.T) {
		t.Parallel()
		var g ingestGate
		g.accepted("h", at(0))
		if !g.tooSoon("h", at(3)) || g.tooSoon("h", at(4)) {
			t.Error("+3 s se descarta y +4 s pasa")
		}
	})
	t.Run("registro acotado", func(t *testing.T) {
		t.Parallel()
		var g ingestGate
		for i := range maxGateHosts + 10 {
			g.accepted(strconv.Itoa(i), at(0))
		}
		if len(g.last) > maxGateHosts {
			t.Errorf("hosts recordados got %d, want <= %d", len(g.last), maxGateHosts)
		}
	})
	t.Run("descartes se agregan en una linea", func(t *testing.T) {
		t.Parallel()
		var g ingestGate
		reports := 0
		for i := range 130 { // 130 descartes en 130 s: 2 resumenes
			if n := g.noteDrop(at(i)); n > 0 {
				reports++
			}
		}
		if reports != 2 {
			t.Errorf("resumenes got %d, want 2", reports)
		}
	})
}

func TestAgentMetricsDropsFastSamples(t *testing.T) {
	t.Parallel()
	const token = "t"
	fs := &fakeStore{validHash: hashToken(token)}
	h, _ := newTestAPI(t, fs)
	body := func(offset int) string {
		return `{"timestamp":` + strconv.FormatInt(fixedNow.Add(time.Duration(offset)*time.Second).Unix(), 10) +
			`,"metrics":{"cpu_pct":1,"mem_used_mb":1,"mem_total_mb":2,"net":{"rx_bytes":1,"tx_bytes":2}}}`
	}
	// -60 pasa, -58 se recorta (200 con dropped), -55 pasa, -54 se recorta.
	steps := []struct {
		offset  int
		dropped bool
	}{{-60, false}, {-58, true}, {-55, false}, {-54, true}}
	for _, s := range steps {
		rec, resp := do(t, h, "POST", "/api/v1/agent/metrics", token, body(s.offset))
		if rec.Code != 200 || resp.Error != nil {
			t.Fatalf("offset %d: status %d error %+v, want 200 (el agente no debe reintentar)", s.offset, rec.Code, resp.Error)
		}
		got := strings.Contains(string(resp.Data), `"dropped":true`)
		if got != s.dropped {
			t.Errorf("offset %d: dropped got %v, want %v (%s)", s.offset, got, s.dropped, resp.Data)
		}
	}
	if len(fs.ingested) != 2 {
		t.Errorf("guardadas got %d, want 2", len(fs.ingested))
	}
}

// --- O3: muestreo de los 429 ---

func TestLimitedSampler(t *testing.T) {
	t.Parallel()
	var s limitedSampler
	base := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	if log, sum := s.observe("1.1.1.1", base); !log || sum != 0 {
		t.Fatalf("primera got log=%v summary=%d, want log sin resumen", log, sum)
	}
	for i := 1; i <= 5; i++ {
		if log, _ := s.observe("1.1.1.1", base.Add(time.Duration(i)*time.Second)); log {
			t.Fatalf("429 repetido %d de la misma IP se logueo", i)
		}
	}
	if log, _ := s.observe("2.2.2.2", base.Add(6*time.Second)); !log {
		t.Error("otra IP debe loguear su primera linea")
	}
	log, sum := s.observe("1.1.1.1", base.Add(61*time.Second))
	if !log || sum != 5 {
		t.Errorf("tras la ventana got log=%v summary=%d, want log y resumen de 5", log, sum)
	}
}

func TestRequestLogSamples429(t *testing.T) {
	t.Parallel()
	const token = "t"
	fs := &fakeStore{validHash: hashToken(token)}
	h, logs := newTestAPI(t, fs)
	// burst 5 del limitador de metricas con reloj fijo: las solicitudes 6..15 son 429.
	limited := 0
	for range 15 {
		rec, _ := do(t, h, "POST", "/api/v1/agent/metrics", token, `{}`)
		if rec.Code == http.StatusTooManyRequests {
			limited++
		}
	}
	if limited != 10 {
		t.Fatalf("429 got %d, want 10", limited)
	}
	if n := strings.Count(logs.String(), `"status":429`); n != 1 {
		t.Errorf("lineas de log con 429 got %d, want 1 (muestreo)", n)
	}
}

// --- B6: /system/status ---

// statusStore sobreescribe SystemStats del fakeStore (que embebe la interfaz Store completa).
type statusStore struct {
	*fakeStore
	stats store.SystemStats
	err   error
}

func (s statusStore) SystemStats(context.Context, time.Time) (store.SystemStats, error) {
	return s.stats, s.err
}

func TestEvaluateChecks(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	src := StatusSources{StartedAt: now.Add(-24 * time.Hour), MaintenanceInterval: 5 * time.Minute, EvalInterval: 15 * time.Second, EvalGrace: 2 * time.Minute}
	ago := func(d time.Duration) beat.State { return beat.State{LastOK: now.Add(-d)} }
	oldest := func(d time.Duration) *time.Time { t := now.Add(-d); return &t }
	healthy := func(in statusInput) statusInput { // completa con latidos sanos lo que el caso no fije
		if in.maint.LastOK.IsZero() {
			in.maint = ago(time.Second)
		}
		if in.eval.LastOK.IsZero() {
			in.eval = ago(time.Second)
		}
		return in
	}

	tests := []struct {
		name  string
		in    statusInput
		want  map[string]string
		total string
	}{
		{name: "todo sano", in: healthy(statusInput{}),
			want: map[string]string{"maintenance": "ok", "evaluator": "ok", "notification_queue": "ok", "notification_dead": "ok"}, total: "ok"},
		{name: "mantenimiento atrasado 3 ciclos", in: healthy(statusInput{maint: ago(15*time.Minute + time.Second)}),
			want: map[string]string{"maintenance": "warn"}, total: "warn"},
		{name: "mantenimiento muy atrasado", in: healthy(statusInput{maint: ago(26 * time.Minute)}),
			want: map[string]string{"maintenance": "crit"}, total: "crit"},
		{name: "mantenimiento en 2 ciclos aun ok", in: healthy(statusInput{maint: ago(10 * time.Minute)}),
			want: map[string]string{"maintenance": "ok"}, total: "ok"},
		{name: "evaluador parado 4 intervalos", in: healthy(statusInput{eval: ago(61 * time.Second)}),
			want: map[string]string{"evaluator": "warn"}, total: "warn"},
		{name: "evaluador parado 11 intervalos", in: healthy(statusInput{eval: ago(166 * time.Second)}),
			want: map[string]string{"evaluator": "crit"}, total: "crit"},
		{name: "sin ciclos aun y en gracia inicial",
			in:   statusInput{now: now, src: StatusSources{StartedAt: now.Add(-time.Minute), MaintenanceInterval: 5 * time.Minute, EvalInterval: 15 * time.Second, EvalGrace: 2 * time.Minute}},
			want: map[string]string{"maintenance": "ok", "evaluator": "ok"}, total: "ok"},
		{name: "sin ciclos desde hace horas", in: statusInput{},
			want: map[string]string{"maintenance": "crit", "evaluator": "crit"}, total: "crit"},
		{name: "cola con pendiente de 11 min", in: healthy(statusInput{stats: store.SystemStats{QueuePending: 3, QueueOldestDue: oldest(11 * time.Minute)}}),
			want: map[string]string{"notification_queue": "warn"}, total: "warn"},
		{name: "cola con pendiente de 31 min", in: healthy(statusInput{stats: store.SystemStats{QueuePending: 3, QueueOldestDue: oldest(31 * time.Minute)}}),
			want: map[string]string{"notification_queue": "crit"}, total: "crit"},
		{name: "cola reciente", in: healthy(statusInput{stats: store.SystemStats{QueuePending: 9, QueueOldestDue: oldest(30 * time.Second)}}),
			want: map[string]string{"notification_queue": "ok"}, total: "ok"},
		{name: "una entrega dead", in: healthy(statusInput{deadTotal: 1}),
			want: map[string]string{"notification_dead": "warn"}, total: "warn"},
		{name: "muchas entregas dead", in: healthy(statusInput{deadTotal: 10}),
			want: map[string]string{"notification_dead": "crit"}, total: "crit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := tt.in
			if in.now.IsZero() {
				in.now, in.src = now, src
			}
			checks := evaluateChecks(in)
			for name, want := range tt.want {
				found := false
				for _, c := range checks {
					if c.Name == name {
						found = true
						if c.Status != want {
							t.Errorf("%s got %s (%s), want %s", c.Name, c.Status, c.Detail, want)
						}
					}
				}
				if !found {
					t.Errorf("falta el chequeo %s", name)
				}
			}
			if got := worstStatus(checks); got != tt.total {
				t.Errorf("estado agregado got %s, want %s", got, tt.total)
			}
		})
	}
}

func newStatusAPI(t *testing.T, ss statusStore, opts Options) (*API, *logBuffer) {
	t.Helper()
	logs := &logBuffer{}
	return New(ss, testHasher(), slog.New(slog.NewJSONHandler(logs, nil)), func() time.Time { return fixedNow }, opts), logs
}

func TestSystemStatusHandler(t *testing.T) {
	t.Parallel()
	var maintBeat, evalBeat beat.Beat
	maintBeat.OK(fixedNow.Add(-time.Minute))
	evalBeat.OK(fixedNow.Add(-5 * time.Second))
	evalBeat.Fail(fixedNow.Add(-20*time.Second), errors.New("boom interno"))
	oldest := fixedNow.Add(-2 * time.Minute)
	gid, gname := "g-1", "Cliente A"
	ss := statusStore{fakeStore: &fakeStore{}, stats: store.SystemStats{
		DBSizeBytes: 123456, QueuePending: 2, QueueOldestDue: &oldest,
		Dead: []store.DeadChannel{{ChannelID: "c-1", Name: "Slack A", GroupID: &gid, GroupName: &gname, Count: 3}, {ChannelID: "c-2", Name: "Global", Count: 1}},
	}}
	api, _ := newStatusAPI(t, ss, Options{Status: StatusSources{
		Version: "1.2.3", StartedAt: fixedNow.Add(-time.Hour), Maintenance: &maintBeat, Evaluator: &evalBeat,
		MaintenanceInterval: 5 * time.Minute, EvalInterval: 15 * time.Second, EvalGrace: 2 * time.Minute,
	}})
	h := api.Routes()

	rec, resp := do(t, h, "GET", "/api/v1/system/status", "", "", asSession)
	if rec.Code != 200 {
		t.Fatalf("status got %d (%s)", rec.Code, rec.Body.String())
	}
	m := dataMap(t, resp)
	if m["status"] != "warn" || m["version"] != "1.2.3" || m["uptime_seconds"] != float64(3600) {
		t.Errorf("cabecera got %v %v %v", m["status"], m["version"], m["uptime_seconds"])
	}
	notif := m["notifications"].(map[string]any)
	if notif["pending"] != float64(2) || notif["oldest_pending_age_seconds"] != float64(120) || notif["dead_24h_total"] != float64(4) {
		t.Errorf("notifications got %v", notif)
	}
	dead := notif["dead_24h"].([]any)
	if len(dead) != 2 {
		t.Fatalf("dead_24h got %v", dead)
	}
	owner := dead[0].(map[string]any)["owner_group"].(map[string]any)
	if owner["name"] != "Cliente A" || dead[1].(map[string]any)["owner_group"] != nil {
		t.Errorf("dead_24h got %v", dead)
	}
	if m["database"].(map[string]any)["size_bytes"] != float64(123456) {
		t.Errorf("database got %v", m["database"])
	}
	ev := m["evaluator"].(map[string]any)
	if ev["last_error"] != "boom interno" || ev["age_seconds"] != float64(5) {
		t.Errorf("evaluator got %v", ev)
	}
	if len(m["checks"].([]any)) != 4 {
		t.Errorf("checks got %v", m["checks"])
	}

	t.Run("un viewer no entra", func(t *testing.T) {
		t.Parallel()
		api, _ := newStatusAPI(t, statusStore{fakeStore: viewerStore(&fakeStore{})}, Options{})
		rec, _ := do(t, api.Routes(), "GET", "/api/v1/system/status", "", "", asSession)
		if rec.Code != http.StatusForbidden {
			t.Errorf("status got %d, want 403", rec.Code)
		}
	})
	t.Run("sin sesion es 401", func(t *testing.T) {
		t.Parallel()
		api, _ := newStatusAPI(t, statusStore{fakeStore: &fakeStore{}}, Options{})
		rec, _ := do(t, api.Routes(), "GET", "/api/v1/system/status", "", "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status got %d, want 401", rec.Code)
		}
	})
	t.Run("fallo de BD es 500 sin detalles", func(t *testing.T) {
		t.Parallel()
		api, logs := newStatusAPI(t, statusStore{fakeStore: &fakeStore{}, err: errors.New("pg secreto")}, Options{})
		rec, resp := do(t, api.Routes(), "GET", "/api/v1/system/status", "", "", asSession)
		wantError(t, rec, resp, http.StatusInternalServerError, "internal_error")
		if strings.Contains(rec.Body.String(), "pg secreto") || !strings.Contains(logs.String(), "pg secreto") {
			t.Error("el error real debe ir al log y no al cliente")
		}
	})
}

func TestStatusLogsCritOnce(t *testing.T) {
	t.Parallel()
	src := StatusSources{StartedAt: fixedNow.Add(-24 * time.Hour), MaintenanceInterval: 5 * time.Minute} // sin ciclos: crit
	api, logs := newStatusAPI(t, statusStore{fakeStore: &fakeStore{}}, Options{Status: src})
	for range 3 {
		if _, _, err := api.collectStatus(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if n := strings.Count(logs.String(), `"level":"ERROR"`); n != 1 {
		t.Errorf("ERROR got %d, want 1 (solo la transicion a crit)", n)
	}
}

// --- agent_outdated ---

func TestAgentOutdatedInAPI(t *testing.T) {
	t.Parallel()
	old, cur, ahead := "0.5.0", "0.6.0", "0.7.0"
	seen := fixedNow.Add(-time.Minute)
	rows := []store.HostRow{
		{ID: "1", Hostname: "viejo", Kind: "agent", Status: "up", StatusChangedAt: seen, AgentVersion: &old},
		{ID: "2", Hostname: "al-dia", Kind: "agent", Status: "up", StatusChangedAt: seen, AgentVersion: &cur},
		{ID: "3", Hostname: "adelantado", Kind: "agent", Status: "up", StatusChangedAt: seen, AgentVersion: &ahead},
		{ID: "4", Hostname: "sin-version", Kind: "agent", Status: "up", StatusChangedAt: seen},
	}
	fs := viewerStore(&fakeStore{
		x: fakeExtra{
			page: store.HostPage{Items: rows, Total: 4},
			summary: store.Summary{AgentVersions: []store.AgentVersionCount{
				{Version: "0.5.0", Hosts: 3}, {Version: "0.4.0", Hosts: 2}, {Version: "0.6.0", Hosts: 7}, {Version: "0.7.0", Hosts: 1}, {Version: "dev", Hosts: 4},
			}},
		},
		detail: store.HostDetail{ID: detailHostID, Hostname: "viejo", Kind: "agent", Status: "up", Info: store.HostInfo{AgentVersion: &old}},
	})
	logger := slog.New(slog.NewJSONHandler(&logBuffer{}, nil))
	withDist := New(fs, testHasher(), logger, func() time.Time { return fixedNow }, Options{Dist: &agentdist.Catalog{Version: "0.6.0"}}).Routes()

	rec, resp := do(t, withDist, "GET", "/api/v1/hosts", "", "", asSession)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var list struct {
		Items []struct {
			Hostname      string `json:"hostname"`
			AgentOutdated bool   `json:"agent_outdated"`
		} `json:"items"`
	}
	if err := json.Unmarshal(resp.Data, &list); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"viejo": true, "al-dia": false, "adelantado": false, "sin-version": false}
	for _, it := range list.Items {
		if it.AgentOutdated != want[it.Hostname] {
			t.Errorf("%s agent_outdated got %v, want %v", it.Hostname, it.AgentOutdated, want[it.Hostname])
		}
	}

	_, resp = do(t, withDist, "GET", "/api/v1/hosts/"+detailHostID, "", "", asSession)
	if info := dataMap(t, resp)["info"].(map[string]any); info["agent_outdated"] != true {
		t.Errorf("detalle agent_outdated got %v, want true", info["agent_outdated"])
	}

	_, resp = do(t, withDist, "GET", "/api/v1/summary", "", "", asSession)
	if av := dataMap(t, resp)["agent_versions"].(map[string]any); av["outdated"] != float64(5) { // 0.5.0 (3) + 0.4.0 (2); dev y >= 0.6.0 no
		t.Errorf("summary agent_versions.outdated got %v, want 5", av["outdated"])
	}

	// Sin catalogo no hay contra que comparar: nunca desactualizado.
	noDist := New(fs, testHasher(), logger, func() time.Time { return fixedNow }, Options{}).Routes()
	_, resp = do(t, noDist, "GET", "/api/v1/hosts/"+detailHostID, "", "", asSession)
	if dataMap(t, resp)["info"].(map[string]any)["agent_outdated"] != false {
		t.Error("sin catalogo agent_outdated debe ser false")
	}
	_, resp = do(t, noDist, "GET", "/api/v1/summary", "", "", asSession)
	if av := dataMap(t, resp)["agent_versions"].(map[string]any); av["outdated"] != float64(0) {
		t.Errorf("summary sin catalogo outdated got %v, want 0", av["outdated"])
	}
}
