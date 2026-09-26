package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"argos/server/internal/auth"
	"argos/server/internal/store"
)

var fixedNow = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

// testSessionID es la cookie de sesion que el fakeStore reconoce (usuario admin salvo que se cambie).
const testSessionID = "sesion-de-prueba"

// fakeStore implementa Store en memoria pa probar validacion, errores y contratos sin DB;
// cada campo err fuerza un fallo. Lo que no implementa (usuarios, login...) se cubre con
// la Postgres real en security_test.go: llamar a un metodo no implementado entra en panico.
type fakeStore struct {
	Store

	mu sync.Mutex

	sessionUser store.User // usuario de testSessionID; vacio = admin
	sessionErr  error
	audits      []store.AuditEntry

	createErr error
	enrollErr error
	authErr   error
	ingestErr error

	ingestResult store.IngestResult // lo que devuelve IngestMetrics (p. ej. down -> up)

	validHash string // hash de token que HostIDByTokenHash reconoce

	enrollHashes []string
	enrollGroup  *string
	ingested     []store.Sample

	// Lista, resumen, grupos, etiquetas y bulk (fake_extra_test.go).
	x fakeExtra

	// Detalle de host y series (hosts_detail_test.go).
	detail    store.HostDetail
	detailErr error
	latest    *store.MetricPoint
	latestErr error
	series    []store.MetricPoint
	seriesErr error
	gotFrom   time.Time
	gotStep   time.Duration
	gotSource string // "raw", "hourly" o "mixed": que consulta de serie uso el handler
}

func (f *fakeStore) CreateEnrollToken(_ context.Context, _, _, _ string, groupID *string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return f.createErr
	}
	f.enrollGroup = groupID
	return nil
}

func (f *fakeStore) SessionByHash(_ context.Context, idHash []byte) (store.SessionInfo, error) {
	if f.sessionErr != nil {
		return store.SessionInfo{}, f.sessionErr
	}
	if string(idHash) != string(auth.HashSessionID(testSessionID)) {
		return store.SessionInfo{}, store.ErrNotFound
	}
	u := f.sessionUser
	if u.ID == "" {
		u = store.User{ID: "u-admin", Username: "admin", Role: store.RoleAdmin}
	}
	return store.SessionInfo{IDHash: idHash, LastSeenAt: fixedNow, ExpiresAt: fixedNow.Add(time.Hour), User: u}, nil
}

func (f *fakeStore) InsertAudit(_ context.Context, e store.AuditEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.audits = append(f.audits, e)
	return nil
}

func (f *fakeStore) EnrollHost(_ context.Context, _, _, _, agentTokenHash string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.enrollErr != nil {
		return "", f.enrollErr
	}
	f.enrollHashes = append(f.enrollHashes, agentTokenHash)
	return "host-1", nil
}

func (f *fakeStore) HostIDByTokenHash(_ context.Context, tokenHash string) (string, error) {
	if f.authErr != nil {
		return "", f.authErr
	}
	if tokenHash != f.validHash {
		return "", store.ErrNotFound
	}
	return "host-1", nil
}

func (f *fakeStore) IngestMetrics(_ context.Context, _ string, s store.Sample) (store.IngestResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ingestErr != nil {
		return store.IngestResult{}, f.ingestErr
	}
	f.ingested = append(f.ingested, s)
	return f.ingestResult, nil
}

// logBuffer captura logs pa afirmar que los 500 se registran.
type logBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// testHasher es compartido: crearlo cuesta un hash Argon2id.
var testHasher = sync.OnceValue(func() *auth.Hasher {
	h, err := auth.NewHasher(auth.DefaultParams, 4)
	if err != nil {
		panic(err)
	}
	return h
})

func newTestAPI(t *testing.T, fs *fakeStore) (http.Handler, *logBuffer) {
	t.Helper()
	logs := &logBuffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	return New(fs, testHasher(), logger, func() time.Time { return fixedNow }, Options{}).Routes(), logs
}

// asSession manda la cookie de sesion que el fakeStore reconoce.
func asSession(r *http.Request) {
	r.AddCookie(&http.Cookie{Name: "__Host-argos_sid", Value: testSessionID})
}

type response struct {
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func do(t *testing.T, h http.Handler, method, path, bearer, body string, mods ...func(*http.Request)) (*httptest.ResponseRecorder, response) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "localhost:8080"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	for _, m := range mods {
		m(req)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var resp response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("respuesta no es envelope JSON: %v (%q)", err, rec.Body.String())
	}
	return rec, resp
}

func wantError(t *testing.T, rec *httptest.ResponseRecorder, resp response, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status got %d, want %d (body %s)", rec.Code, status, rec.Body.String())
	}
	if resp.Error == nil || resp.Error.Code != code {
		t.Fatalf("error got %+v, want code %q", resp.Error, code)
	}
	if string(resp.Data) != "null" {
		t.Errorf("data got %s, want null", resp.Data)
	}
}

func TestEnrollToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		createErr  error
		wantStatus int
		wantCode   string
	}{
		{name: "feliz", body: `{"hostname":"srv-01"}`, wantStatus: 201},
		{name: "hostname con espacios se recorta", body: `{"hostname":"  srv-01  "}`, wantStatus: 201},
		{name: "hostname vacio", body: `{"hostname":"   "}`, wantStatus: 400, wantCode: "invalid_body"},
		{name: "hostname largo", body: `{"hostname":"` + strings.Repeat("a", 254) + `"}`, wantStatus: 400, wantCode: "invalid_body"},
		{name: "hostname con NUL", body: `{"hostname":"a\u0000b"}`, wantStatus: 400, wantCode: "invalid_body"},
		{name: "campo desconocido", body: `{"hostname":"x","otro":1}`, wantStatus: 400, wantCode: "invalid_body"},
		{name: "json roto", body: `{`, wantStatus: 400, wantCode: "invalid_body"},
		{name: "body vacio", body: ``, wantStatus: 400, wantCode: "invalid_body"},
		{name: "dos documentos", body: `{"hostname":"a"}{"hostname":"b"}`, wantStatus: 400, wantCode: "invalid_body"},
		{name: "body enorme", body: `{"hostname":"` + strings.Repeat("a", 5000) + `"}`, wantStatus: 413, wantCode: "payload_too_large"},
		{name: "error de db es 500", body: `{"hostname":"x"}`, createErr: errors.New("boom"), wantStatus: 500, wantCode: "internal_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := &fakeStore{createErr: tt.createErr}
			h, _ := newTestAPI(t, fs)
			rec, resp := do(t, h, "POST", "/api/v1/hosts/enroll-token", "", tt.body, asSession)
			if tt.wantCode != "" {
				wantError(t, rec, resp, tt.wantStatus, tt.wantCode)
				return
			}
			if rec.Code != tt.wantStatus {
				t.Fatalf("status got %d, want %d", rec.Code, tt.wantStatus)
			}
			var data struct {
				EnrollToken string    `json:"enroll_token"`
				ExpiresAt   time.Time `json:"expires_at"`
			}
			if err := json.Unmarshal(resp.Data, &data); err != nil {
				t.Fatal(err)
			}
			if len(data.EnrollToken) != 32 {
				t.Errorf("enroll_token len got %d, want 32", len(data.EnrollToken))
			}
			if want := fixedNow.Add(enrollTokenTTL); !data.ExpiresAt.Equal(want) {
				t.Errorf("expires_at got %v, want %v", data.ExpiresAt, want)
			}
		})
	}
}

func TestAgentEnroll(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		enrollErr  error
		wantStatus int
		wantCode   string
	}{
		{name: "feliz", body: `{"enroll_token":"abc","os":"linux","arch":"amd64"}`, wantStatus: 201},
		{name: "os y arch opcionales", body: `{"enroll_token":"abc"}`, wantStatus: 201},
		{name: "sin token", body: `{"os":"linux"}`, wantStatus: 400, wantCode: "invalid_body"},
		{name: "token con NUL", body: `{"enroll_token":"a\u0000"}`, wantStatus: 400, wantCode: "invalid_body"},
		{name: "os largo", body: `{"enroll_token":"a","os":"` + strings.Repeat("x", 65) + `"}`, wantStatus: 400, wantCode: "invalid_body"},
		{name: "token inexistente", body: `{"enroll_token":"abc"}`, enrollErr: store.ErrNotFound, wantStatus: 401, wantCode: "invalid_token"},
		// Un unico 401 invalid_token pa inexistente/usado/expirado (el motivo real solo va al log).
		{name: "token usado", body: `{"enroll_token":"abc"}`, enrollErr: store.ErrTokenUsed, wantStatus: 401, wantCode: "invalid_token"},
		{name: "token expirado", body: `{"enroll_token":"abc"}`, enrollErr: store.ErrTokenExpired, wantStatus: 401, wantCode: "invalid_token"},
		{name: "sentinela envuelta", body: `{"enroll_token":"abc"}`, enrollErr: errors.Join(errors.New("ctx"), store.ErrTokenUsed), wantStatus: 401, wantCode: "invalid_token"},
		{name: "error de db es 500", body: `{"enroll_token":"abc"}`, enrollErr: errors.New("boom"), wantStatus: 500, wantCode: "internal_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := &fakeStore{enrollErr: tt.enrollErr}
			h, _ := newTestAPI(t, fs)
			rec, resp := do(t, h, "POST", "/api/v1/agent/enroll", "", tt.body)
			if tt.wantCode != "" {
				wantError(t, rec, resp, tt.wantStatus, tt.wantCode)
				return
			}
			if rec.Code != tt.wantStatus {
				t.Fatalf("status got %d, want %d", rec.Code, tt.wantStatus)
			}
			var data struct {
				HostID     string `json:"host_id"`
				AgentToken string `json:"agent_token"`
			}
			if err := json.Unmarshal(resp.Data, &data); err != nil {
				t.Fatal(err)
			}
			if data.HostID != "host-1" || len(data.AgentToken) != 64 {
				t.Errorf("data got %+v, want host-1 y token de 64 hex", data)
			}
			// Lo que llega al store es el hash, nunca el token en claro.
			if len(fs.enrollHashes) != 1 || fs.enrollHashes[0] != hashToken(data.AgentToken) {
				t.Errorf("el store no recibio el hash del agent token")
			}
		})
	}
}

func TestAgentMetrics(t *testing.T) {
	t.Parallel()

	const token = "secreto-valido"
	ok := `{"timestamp":` + strconv.FormatInt(fixedNow.Unix(), 10) + `,"metrics":{"cpu_pct":12.5,"mem_used_mb":100,"mem_total_mb":200,"net":{"rx_bytes":1,"tx_bytes":2}},"services":[{"name":"nginx","status":"up","response_ms":5}]}`
	manyServices := `{"services":[` + strings.TrimSuffix(strings.Repeat(`{"name":"s","status":"up"},`, maxServices+1), ",") + `]}`

	tests := []struct {
		name       string
		bearer     string
		body       string
		authErr    error
		ingestErr  error
		wantStatus int
		wantCode   string
		wantIngest int
	}{
		{name: "feliz", bearer: token, body: ok, wantStatus: 200, wantIngest: 1},
		{name: "timestamp 0 usa now", bearer: token, body: `{}`, wantStatus: 200, wantIngest: 1},
		{name: "campos extra del agente se toleran", bearer: token, body: `{"agent_version":"1.0","hostname":"h"}`, wantStatus: 200, wantIngest: 1},
		{name: "sin header", body: ok, wantStatus: 401, wantCode: "invalid_token"},
		{name: "token invalido", bearer: "otro", body: ok, wantStatus: 401, wantCode: "invalid_token"},
		{name: "token gigante", bearer: strings.Repeat("a", 300), body: ok, wantStatus: 401, wantCode: "invalid_token"},
		{name: "error de db en auth es 500 no 401", bearer: token, body: ok, authErr: errors.New("db caida"), wantStatus: 500, wantCode: "internal_error"},
		{name: "error de db en ingest es 500", bearer: token, body: ok, ingestErr: errors.New("db caida"), wantStatus: 500, wantCode: "internal_error"},
		{name: "sin particion pa el timestamp es 503 reintentable", bearer: token, body: ok,
			ingestErr: fmt.Errorf("ingest de metricas: %w: db caida", store.ErrPartitionMissing), wantStatus: 503, wantCode: "service_unavailable"},
		{name: "body enorme", bearer: token, body: `{"services":[{"name":"` + strings.Repeat("a", maxMetricsBytes) + `"}]}`, wantStatus: 413, wantCode: "payload_too_large"},
		{name: "json roto", bearer: token, body: `{`, wantStatus: 400, wantCode: "invalid_body"},
		{name: "timestamp absurdo futuro", bearer: token, body: `{"timestamp":99999999999}`, wantStatus: 400, wantCode: "invalid_body"},
		{name: "timestamp absurdo pasado", bearer: token, body: `{"timestamp":1}`, wantStatus: 400, wantCode: "invalid_body"},
		{name: "timestamp negativo", bearer: token, body: `{"timestamp":-5}`, wantStatus: 400, wantCode: "invalid_body"},
		{name: "cpu fuera de rango", bearer: token, body: `{"metrics":{"cpu_pct":101}}`, wantStatus: 400, wantCode: "invalid_body"},
		{name: "mem desborda int32", bearer: token, body: `{"metrics":{"mem_used_mb":3000000000}}`, wantStatus: 400, wantCode: "invalid_body"},
		{name: "cpu negativo", bearer: token, body: `{"metrics":{"cpu_pct":-0.5}}`, wantStatus: 400, wantCode: "invalid_body"},
		{name: "mem usada mayor que la total", bearer: token, body: `{"metrics":{"mem_used_mb":201,"mem_total_mb":200}}`, wantStatus: 400, wantCode: "invalid_body"},
		{name: "mem usada igual a la total", bearer: token, body: `{"metrics":{"mem_used_mb":200,"mem_total_mb":200}}`, wantStatus: 200, wantIngest: 1},
		{name: "bateria fuera de rango", bearer: token, body: `{"metrics":{"battery":{"pct":101}}}`, wantStatus: 400, wantCode: "invalid_body"},
		{name: "disco usado mayor que el total", bearer: token, body: `{"disks":[{"mount":"/","used_mb":11,"total_mb":10}]}`, wantStatus: 400, wantCode: "invalid_body"},
		{name: "net negativo", bearer: token, body: `{"metrics":{"net":{"rx_bytes":-1}}}`, wantStatus: 400, wantCode: "invalid_body"},
		{name: "demasiados services", bearer: token, body: manyServices, wantStatus: 400, wantCode: "invalid_body"},
		{name: "service sin nombre", bearer: token, body: `{"services":[{"name":"","status":"up"}]}`, wantStatus: 400, wantCode: "invalid_body"},
		{name: "service nombre largo", bearer: token, body: `{"services":[{"name":"` + strings.Repeat("a", 101) + `","status":"up"}]}`, wantStatus: 400, wantCode: "invalid_body"},
		{name: "service status invalido", bearer: token, body: `{"services":[{"name":"a","status":"roto"}]}`, wantStatus: 400, wantCode: "invalid_body"},
		{name: "service response_ms negativo", bearer: token, body: `{"services":[{"name":"a","status":"up","response_ms":-1}]}`, wantStatus: 400, wantCode: "invalid_body"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := &fakeStore{validHash: hashToken(token), authErr: tt.authErr, ingestErr: tt.ingestErr}
			h, logs := newTestAPI(t, fs)
			rec, resp := do(t, h, "POST", "/api/v1/agent/metrics", tt.bearer, tt.body)
			if tt.wantCode != "" {
				wantError(t, rec, resp, tt.wantStatus, tt.wantCode)
			} else if rec.Code != tt.wantStatus {
				t.Fatalf("status got %d, want %d (body %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if len(fs.ingested) != tt.wantIngest {
				t.Errorf("ingested got %d, want %d", len(fs.ingested), tt.wantIngest)
			}
			if tt.wantStatus == 500 || tt.wantStatus == 503 {
				if !strings.Contains(logs.String(), "db caida") {
					t.Errorf("el 500 no logueo el error real: %q", logs.String())
				}
				if strings.Contains(rec.Body.String(), "db caida") {
					t.Errorf("el error real se filtro al cliente: %s", rec.Body.String())
				}
			}
		})
	}
}

func TestAgentMetricsLogsRecovery(t *testing.T) {
	t.Parallel()

	const token = "secreto-valido"
	tests := []struct {
		name string
		res  store.IngestResult
		want bool
	}{
		{name: "host caido que vuelve se loguea", res: store.IngestResult{Hostname: "srv-01", Recovered: true, PreviousStatus: "down"}, want: true},
		{name: "host offline que vuelve (laptop que despierta) no ensucia el log", res: store.IngestResult{Hostname: "srv-01", Recovered: true, PreviousStatus: "offline"}, want: false},
		{name: "host que ya estaba up no loguea", res: store.IngestResult{Hostname: "srv-01"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, logs := newTestAPI(t, &fakeStore{validHash: hashToken(token), ingestResult: tt.res})
			rec, _ := do(t, h, "POST", "/api/v1/agent/metrics", token, `{}`)
			if rec.Code != 200 {
				t.Fatalf("status got %d, want 200", rec.Code)
			}
			got := logs.String()
			if has := strings.Contains(got, "host recuperado"); has != tt.want {
				t.Errorf("log de recuperacion got %v, want %v (logs %q)", has, tt.want, got)
			}
			if tt.want && (!strings.Contains(got, `"hostname":"srv-01"`) || !strings.Contains(got, `"host_id":"host-1"`)) {
				t.Errorf("el log debe llevar hostname y host_id: %q", got)
			}
			if strings.Contains(got, token) {
				t.Errorf("el log filtro el token del agente: %q", got)
			}
		})
	}
}

func TestAgentMetricsSampleContent(t *testing.T) {
	t.Parallel()

	const token = "t"
	fs := &fakeStore{validHash: hashToken(token)}
	h, _ := newTestAPI(t, fs)
	body := `{"timestamp":` + strconv.FormatInt(fixedNow.Unix(), 10) + `,"metrics":{"cpu_pct":12.5,"mem_used_mb":100,"mem_total_mb":200,"net":{"rx_bytes":1,"tx_bytes":2}},"services":[{"name":"nginx","status":"up","response_ms":5}]}`
	rec, _ := do(t, h, "POST", "/api/v1/agent/metrics", token, body)
	if rec.Code != 200 {
		t.Fatalf("status got %d, want 200", rec.Code)
	}
	if len(fs.ingested) != 1 {
		t.Fatalf("ingested got %d, want 1", len(fs.ingested))
	}
	got := fs.ingested[0]
	want := store.Sample{
		Timestamp: time.Unix(fixedNow.Unix(), 0), CPUPct: 12.5, MemUsedMB: 100, MemTotalMB: 200,
		NetRxBytes: 1, NetTxBytes: 2,
		Services: []store.ServiceCheck{{Name: "nginx", Status: "up", ResponseMs: 5}},
	}
	if !got.Timestamp.Equal(want.Timestamp) || got.CPUPct != want.CPUPct || got.MemUsedMB != want.MemUsedMB ||
		got.MemTotalMB != want.MemTotalMB || got.NetRxBytes != want.NetRxBytes || got.NetTxBytes != want.NetTxBytes ||
		len(got.Services) != 1 || got.Services[0] != want.Services[0] {
		t.Errorf("sample got %+v, want %+v", got, want)
	}
}

func TestHealth(t *testing.T) {
	t.Parallel()
	h, _ := newTestAPI(t, &fakeStore{})
	rec, _ := do(t, h, "GET", "/health", "", "")
	if rec.Code != 200 {
		t.Errorf("status got %d, want 200", rec.Code)
	}
}
