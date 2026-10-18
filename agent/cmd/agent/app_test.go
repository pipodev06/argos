package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"argos/agent/internal/config"
	"argos/agent/internal/sender"
	"argos/agent/internal/service"
	"argos/agent/internal/state"
)

const (
	testEnrollToken = "enroll-token-secreto"
	testAgentToken  = "agent-token-secreto"
	testHostID      = "11111111-2222-3333-4444-555555555555"
)

type fakeCollector struct{}

func (fakeCollector) Collect(context.Context) (sender.Metrics, error) {
	return sender.Metrics{CPUPct: 5, MemUsedMB: 1, MemTotalMB: 2, Net: sender.NetMetrics{RxBytes: 10, TxBytes: 20}}, nil
}

// fakeServer imita enroll y metrics del server real (solo lo que el agente usa).
type fakeServer struct {
	*httptest.Server
	mu          sync.Mutex
	enrollBody  map[string]any
	metricsAuth []string
	metricsBody []map[string]any
	metricsCode int           // status de metrics (default 200)
	got         chan struct{} // una senal por request de metrics
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	fs := &fakeServer{metricsCode: http.StatusOK, got: make(chan struct{}, 100)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/agent/enroll", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		fs.mu.Lock()
		fs.enrollBody = body
		fs.mu.Unlock()
		if body["enroll_token"] != testEnrollToken {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"data":null,"error":{"code":"invalid_token","message":"token invalido o revocado"}}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"host_id":"` + testHostID + `","agent_token":"` + testAgentToken + `"},"error":null}`))
	})
	mux.HandleFunc("POST /api/v1/agent/metrics", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		fs.mu.Lock()
		fs.metricsAuth = append(fs.metricsAuth, r.Header.Get("Authorization"))
		fs.metricsBody = append(fs.metricsBody, body)
		code := fs.metricsCode
		fs.mu.Unlock()
		w.WriteHeader(code)
		fs.got <- struct{}{}
	})
	fs.Server = httptest.NewServer(mux)
	t.Cleanup(fs.Close)
	return fs
}

func (fs *fakeServer) waitMetrics(t *testing.T) {
	t.Helper()
	select {
	case <-fs.got:
	case <-time.After(10 * time.Second):
		t.Fatal("el agente no envio metricas")
	}
}

// fakeSvc es un servicio del SO simulado: registra las llamadas y no toca el sistema.
type fakeSvc struct {
	mu        sync.Mutex
	installed bool
	stateDir  string
	calls     []string
	opts      service.InstallOptions
	keepState bool
	err       error
	status    string // texto de Status; vacio = detenido
}

func (f *fakeSvc) record(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name)
	return f.err
}

func (f *fakeSvc) Install(_ context.Context, o service.InstallOptions) error {
	f.opts = o
	return f.record("install")
}

func (f *fakeSvc) Uninstall(_ context.Context, keep bool, _ io.Writer) error {
	f.keepState = keep
	return f.record("uninstall")
}
func (f *fakeSvc) Start(context.Context) error { return f.record("start") }
func (f *fakeSvc) Stop(context.Context) error  { return f.record("stop") }
func (f *fakeSvc) Status(context.Context) (string, error) {
	if f.status != "" {
		return f.status, f.record("status")
	}
	return "instalado; estado: detenido", f.record("status")
}
func (f *fakeSvc) Upgrade(context.Context, service.InstallOptions) error { return f.record("upgrade") }
func (f *fakeSvc) Installed() bool                                       { return f.installed }
func (f *fakeSvc) StateDir() string                                      { return f.stateDir }
func (f *fakeSvc) StateAccount() string                                  { return "" }
func (f *fakeSvc) ExePath() (string, error)                              { return filepath.Join(f.stateDir, "argos-agent"), nil }
func (f *fakeSvc) RequirePrivileges() error                              { return nil }

type harness struct {
	app    *app
	stdin  *bytes.Buffer
	stdout *bytes.Buffer
	stderr *bytes.Buffer
	logs   *bytes.Buffer
	dir    string
	env    map[string]string
	svc    *fakeSvc
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		stdin: &bytes.Buffer{}, stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, logs: &bytes.Buffer{},
		dir: t.TempDir(), env: map[string]string{}, svc: &fakeSvc{stateDir: filepath.Join(t.TempDir(), "servicio")},
	}
	h.env["ARGOS_STATE_DIR"] = h.dir
	h.app = &app{
		version:   "test",
		getenv:    func(k string) string { return h.env[k] },
		stdin:     h.stdin,
		stdout:    h.stdout,
		stderr:    h.stderr,
		logger:    slog.New(slog.NewJSONHandler(h.logs, nil)),
		collector: fakeCollector{},
		svc:       h.svc,
	}
	return h
}

// assertNoSecrets falla si algun secreto aparece en stdout o en los logs.
func (h *harness) assertNoSecrets(t *testing.T) {
	t.Helper()
	for _, secret := range []string{testEnrollToken, testAgentToken} {
		if strings.Contains(h.stdout.String(), secret) || strings.Contains(h.logs.String(), secret) {
			t.Errorf("un secreto (%s...) aparecio en stdout o logs:\nstdout=%s\nlogs=%s", secret[:6], h.stdout, h.logs)
		}
	}
}

func TestEnrollThenRunUsesTokenFromFile(t *testing.T) {
	t.Parallel()
	srv := newFakeServer(t)
	h := newHarness(t)

	if err := h.app.execute(context.Background(), []string{"enroll", "--server", srv.URL, "--token", testEnrollToken}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if !strings.Contains(h.stdout.String(), testHostID) {
		t.Errorf("stdout deberia mostrar el host_id: %q", h.stdout)
	}
	srv.mu.Lock()
	if srv.enrollBody["os"] != runtime.GOOS || srv.enrollBody["arch"] != runtime.GOARCH {
		t.Errorf("enroll body got %v, want os/arch de runtime", srv.enrollBody)
	}
	srv.mu.Unlock()

	if runtime.GOOS != "windows" {
		fi, err := os.Stat(state.New(h.dir, h.app.logger).Path())
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("archivo de estado: err=%v perm=%v, want 0600", err, fi)
		}
	}

	// run sin ARGOS_SERVER_URL ni ARGOS_AGENT_TOKEN: todo sale del archivo.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.app.execute(ctx, nil) }()
	srv.waitMetrics(t)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.metricsAuth[0] != "Bearer "+testAgentToken {
		t.Errorf("Authorization got %q, want el token del archivo", srv.metricsAuth[0])
	}
	if b, _ := json.Marshal(srv.metricsBody[0]); strings.Contains(string(b), testAgentToken) {
		t.Errorf("el token no debe ir en el body: %s", b)
	}
	h.assertNoSecrets(t)
}

func TestRunEnvTokenOverridesFile(t *testing.T) {
	t.Parallel()
	srv := newFakeServer(t)
	h := newHarness(t)
	if err := state.New(h.dir, h.app.logger).Save(state.Credentials{HostID: "h", AgentToken: "del-archivo", ServerURL: srv.URL}); err != nil {
		t.Fatal(err)
	}
	h.env["ARGOS_AGENT_TOKEN"] = "del-env"
	h.env["ARGOS_SERVER_URL"] = srv.URL // el override de token no hereda la URL del archivo

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.app.execute(ctx, []string{"run"}) }()
	srv.waitMetrics(t)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.metricsAuth[0] != "Bearer del-env" {
		t.Errorf("Authorization got %q, want el override de env", srv.metricsAuth[0])
	}
}

func TestRunWithoutTokenExplainsHowToEnroll(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.env["ARGOS_SERVER_URL"] = "http://127.0.0.1:1"

	err := h.app.execute(context.Background(), nil)
	if !errors.Is(err, state.ErrNotEnrolled) || !strings.Contains(err.Error(), "argos-agent enroll") {
		t.Errorf("err got %v, want ErrNotEnrolled con instrucciones de enroll", err)
	}
}

func TestRunRefusesRemoteHTTPUnlessAllowed(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.env["ARGOS_SERVER_URL"] = "http://argos.remoto.example:8080"
	h.env["ARGOS_AGENT_TOKEN"] = testAgentToken

	err := h.app.execute(context.Background(), []string{"run"})
	if !errors.Is(err, config.ErrInsecureTransport) {
		t.Fatalf("err got %v, want ErrInsecureTransport", err)
	}
	h.assertNoSecrets(t)

	// Con ARGOS_ALLOW_INSECURE=1 arranca (aqui se corta enseguida) y advierte fuerte.
	h.env["ARGOS_ALLOW_INSECURE"] = "1"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := h.app.execute(ctx, []string{"run"}); err != nil {
		t.Fatalf("con ALLOW_INSECURE: %v", err)
	}
	if !strings.Contains(h.logs.String(), "ARGOS_ALLOW_INSECURE activo") {
		t.Errorf("falta la advertencia fuerte en el log: %s", h.logs)
	}
	h.assertNoSecrets(t)
}

func TestEnrollRefusesRemoteHTTP(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	err := h.app.execute(context.Background(), []string{"enroll", "--server", "http://argos.remoto.example", "--token", testEnrollToken})
	if !errors.Is(err, config.ErrInsecureTransport) {
		t.Errorf("err got %v, want ErrInsecureTransport", err)
	}
	h.assertNoSecrets(t)
}

func TestEnrollInvalidTokenGivesClearErrorWithoutLeaking(t *testing.T) {
	t.Parallel()
	srv := newFakeServer(t)
	h := newHarness(t)
	h.env["ARGOS_SERVER_URL"] = srv.URL
	h.env["ARGOS_ENROLL_TOKEN"] = "otro-token-malo-xyz"

	err := h.app.execute(context.Background(), []string{"enroll"})
	if err == nil || !strings.Contains(err.Error(), "invalido") {
		t.Fatalf("err got %v, want mensaje claro de token invalido", err)
	}
	if strings.Contains(err.Error(), "otro-token-malo-xyz") {
		t.Errorf("el error revela el token: %v", err)
	}
	if _, loadErr := state.New(h.dir, h.app.logger).Load(); !errors.Is(loadErr, state.ErrNotEnrolled) {
		t.Errorf("un enroll fallido no debe dejar estado: %v", loadErr)
	}
}

func TestEnrollExistingRequiresForce(t *testing.T) {
	t.Parallel()
	srv := newFakeServer(t)
	h := newHarness(t)
	args := []string{"enroll", "--server", srv.URL, "--token", testEnrollToken}

	if err := h.app.execute(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	err := h.app.execute(context.Background(), args)
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("segundo enroll sin --force: err got %v", err)
	}
	if err := h.app.execute(context.Background(), append(args, "--force")); err != nil {
		t.Fatalf("enroll con --force: %v", err)
	}
	h.assertNoSecrets(t)
}

func TestEnrollMissingInputs(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	if err := h.app.execute(context.Background(), []string{"enroll", "--token", "x"}); err == nil || !strings.Contains(err.Error(), "servidor") {
		t.Errorf("sin servidor: err got %v", err)
	}
	if err := h.app.execute(context.Background(), []string{"enroll", "--server", "https://a.example"}); err == nil || !strings.Contains(err.Error(), "enroll token") {
		t.Errorf("sin token: err got %v", err)
	}
}

func TestRunRevokedTokenStopsWithTokenRejected(t *testing.T) {
	t.Parallel()
	srv := newFakeServer(t)
	srv.metricsCode = http.StatusUnauthorized
	h := newHarness(t)
	h.env["ARGOS_SERVER_URL"] = srv.URL
	h.env["ARGOS_AGENT_TOKEN"] = testAgentToken

	done := make(chan error, 1)
	go func() { done <- h.app.execute(context.Background(), nil) }()
	select {
	case err := <-done:
		if !errors.Is(err, sender.ErrTokenRejected) {
			t.Fatalf("err got %v, want ErrTokenRejected", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("el agente no se detuvo tras un 401")
	}
	if !strings.Contains(h.logs.String(), "re-enrola el agente") {
		t.Errorf("falta el mensaje claro en el log: %s", h.logs)
	}
	srv.mu.Lock()
	n := len(srv.metricsAuth)
	srv.mu.Unlock()
	if n != 1 {
		t.Errorf("requests got %d, want 1 (un 401 no se reintenta)", n)
	}
	h.assertNoSecrets(t)
}

func TestUnknownSubcommand(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	if err := h.app.execute(context.Background(), []string{"bogus"}); err == nil {
		t.Error("se esperaba error por subcomando desconocido")
	}
}
