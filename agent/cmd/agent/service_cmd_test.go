package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"argos/agent/internal/agent"
	"argos/agent/internal/sender"
	"argos/agent/internal/service"
	"argos/agent/internal/state"
)

// syncBuffer es un bytes.Buffer seguro pa que el logger (otra goroutine) y el test lo usen a la vez.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// waitFor sondea cond hasta que se cumpla o venza el plazo.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("no se cumplio a tiempo: %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestEnrollTokenFromStdinNeverLeaks(t *testing.T) {
	t.Parallel()
	srv := newFakeServer(t)
	h := newHarness(t)
	h.stdin.WriteString(testEnrollToken + "\r\n") // con CRLF, como en Windows

	if err := h.app.execute(context.Background(), []string{"enroll", "--server", srv.URL, "--token-stdin"}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	srv.mu.Lock()
	got := srv.enrollBody["enroll_token"]
	srv.mu.Unlock()
	if got != testEnrollToken {
		t.Errorf("token enviado al server got %v, want el de stdin sin saltos de linea", got)
	}
	if !strings.Contains(h.stdout.String(), testHostID) {
		t.Errorf("stdout deberia mostrar el host_id: %q", h.stdout)
	}
	// Ni el enroll token ni el agent token en stdout, stderr o logs.
	h.assertNoSecrets(t)
}

func TestEnrollTokenStdinFailuresNeverLeak(t *testing.T) {
	t.Parallel()
	srv := newFakeServer(t)

	tests := []struct {
		name    string
		stdin   string
		args    []string
		wantErr string
	}{
		{"stdin vacio", "", []string{"--token-stdin"}, "ningun token"},
		{"solo espacios", "  \n", []string{"--token-stdin"}, "ningun token"},
		{"ambos", testEnrollToken + "\n", []string{"--token-stdin", "--token", testEnrollToken}, "no ambos"},
		{"token malo", "token-invalido-xyz\n", []string{"--token-stdin"}, "invalido"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			h.stdin.WriteString(tt.stdin)
			err := h.app.execute(context.Background(), append([]string{"enroll", "--server", srv.URL}, tt.args...))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err got %v, want que contenga %q", err, tt.wantErr)
			}
			for _, secret := range []string{testEnrollToken, "token-invalido-xyz"} {
				if strings.Contains(err.Error(), secret) || strings.Contains(h.stdout.String(), secret) || strings.Contains(h.logs.String(), secret) {
					t.Errorf("el token (%s...) se filtro:\nerr=%v\nstdout=%s\nlogs=%s", secret[:6], err, h.stdout, h.logs)
				}
			}
		})
	}
}

func TestEnrollTokenOnArgvWarnsWithoutEchoingIt(t *testing.T) {
	t.Parallel()
	srv := newFakeServer(t)
	h := newHarness(t)

	if err := h.app.execute(context.Background(), []string{"enroll", "--server", srv.URL, "--token", testEnrollToken}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.stderr.String(), "--token-stdin") {
		t.Errorf("falta la advertencia en stderr: %q", h.stderr)
	}
	h.assertNoSecrets(t) // la advertencia no repite el valor
}

func TestEnrollUsesServiceDirWhenServiceInstalled(t *testing.T) {
	t.Parallel()
	srv := newFakeServer(t)
	h := newHarness(t)
	delete(h.env, "ARGOS_STATE_DIR")
	h.svc.installed = true
	h.stdin.WriteString(testEnrollToken + "\n")

	if err := h.app.execute(context.Background(), []string{"enroll", "--server", srv.URL, "--token-stdin"}); err != nil {
		t.Fatal(err)
	}
	if _, err := state.New(h.svc.stateDir, h.app.logger).Load(); err != nil {
		t.Errorf("el estado debe quedar en el directorio del servicio: %v", err)
	}
	if !strings.Contains(h.stdout.String(), "argos-agent start") {
		t.Errorf("debe sugerir arrancar el servicio: %q", h.stdout)
	}
	h.assertNoSecrets(t)
}

func TestRunUsesCAFromStateDirByDefault(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	if err := os.WriteFile(filepath.Join(h.dir, state.CAFileName), []byte("no es un pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.env["ARGOS_SERVER_URL"] = "http://127.0.0.1:1"
	h.env["ARGOS_AGENT_TOKEN"] = testAgentToken
	// Como ca.pem existe y es invalida, el agente debe intentar usarla (y quejarse), lo que prueba que la toma por defecto.
	err := h.app.execute(context.Background(), []string{"run"})
	if err == nil || !strings.Contains(err.Error(), "ARGOS_CA_FILE") {
		t.Errorf("err got %v, want error por la ca.pem del estado", err)
	}
}

func TestSubcommandsDelegateToServiceManager(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()

	h.env["ARGOS_CA_FILE"] = "/tmp/ca-desde-env.pem"
	if err := h.app.execute(ctx, []string{"install"}); err != nil {
		t.Fatal(err)
	}
	if h.svc.opts.CAFile != "/tmp/ca-desde-env.pem" {
		t.Errorf("CAFile got %q, want el de ARGOS_CA_FILE", h.svc.opts.CAFile)
	}
	if err := h.app.execute(ctx, []string{"install", "--ca-file", "/tmp/ca-flag.pem"}); err != nil {
		t.Fatal(err)
	}
	if h.svc.opts.CAFile != "/tmp/ca-flag.pem" {
		t.Errorf("CAFile got %q, want el del flag (gana al env)", h.svc.opts.CAFile)
	}
	if !strings.Contains(h.stdout.String(), "--token-stdin") {
		t.Errorf("install debe indicar el siguiente paso: %q", h.stdout)
	}

	if err := h.app.execute(ctx, []string{"uninstall"}); err != nil || h.svc.keepState {
		t.Errorf("uninstall por defecto borra el estado: err %v keepState %v", err, h.svc.keepState)
	}
	if err := h.app.execute(ctx, []string{"uninstall", "--keep-state"}); err != nil || !h.svc.keepState {
		t.Errorf("uninstall --keep-state: err %v keepState %v", err, h.svc.keepState)
	}
	if err := h.app.execute(ctx, []string{"stop"}); err != nil {
		t.Fatal(err)
	}
	if err := h.app.execute(ctx, []string{"status"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.stdout.String(), "no enrolado") {
		t.Errorf("status debe informar el enroll: %q", h.stdout)
	}
	want := []string{"install", "install", "uninstall", "uninstall", "stop", "status"}
	if strings.Join(h.svc.calls, ",") != strings.Join(want, ",") {
		t.Errorf("llamadas got %v, want %v", h.svc.calls, want)
	}
}

func TestServiceCommandErrorsAndArgs(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()

	h.svc.err = service.ErrNotPrivileged
	if err := h.app.execute(ctx, []string{"install"}); !errors.Is(err, service.ErrNotPrivileged) {
		t.Errorf("install sin privilegios: err got %v, want ErrNotPrivileged envuelto", err)
	}
	h.svc.err = nil
	for _, args := range [][]string{{"start", "extra"}, {"stop", "x"}, {"status", "x"}, {"install", "x"}, {"uninstall", "x"}, {"run", "x"}} {
		if err := h.app.execute(ctx, args); err == nil {
			t.Errorf("%v: se esperaba error por argumentos sobrantes", args)
		}
	}
}

func TestStartRequiresEnrollment(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	err := h.app.execute(context.Background(), []string{"start"})
	if err == nil || !strings.Contains(err.Error(), "enroll") {
		t.Fatalf("start sin enroll: err got %v", err)
	}
	if len(h.svc.calls) != 0 {
		t.Errorf("no debe llamar al servicio sin enroll: %v", h.svc.calls)
	}
	if err := state.New(h.svc.stateDir, h.app.logger).Save(state.Credentials{HostID: "h", AgentToken: "t"}); err != nil {
		t.Fatal(err)
	}
	if err := h.app.execute(context.Background(), []string{"start"}); err != nil {
		t.Fatal(err)
	}
	if len(h.svc.calls) != 1 || h.svc.calls[0] != "start" {
		t.Errorf("llamadas got %v, want [start]", h.svc.calls)
	}
}

// En modo servicio un 401 NO termina el proceso: queda inactivo con un log periodico.
func TestServiceModeRejectedTokenStaysIdleAndLogsPeriodically(t *testing.T) {
	t.Parallel()
	srv := newFakeServer(t)
	srv.metricsCode = http.StatusUnauthorized
	h := newHarness(t)
	logs := &syncBuffer{}
	h.app.logger = slog.New(slog.NewJSONHandler(logs, nil))
	h.app.idleLogEvery, h.app.idleRecheck = 40*time.Millisecond, 10*time.Millisecond
	h.env["ARGOS_SERVER_URL"] = srv.URL
	h.env["ARGOS_AGENT_TOKEN"] = testAgentToken

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.app.execute(ctx, []string{"run", "--service"}) }()

	waitFor(t, "al menos 3 avisos periodicos de token rechazado", func() bool {
		return strings.Count(logs.String(), "token rechazado: re-enrola el agente") >= 3
	})
	select {
	case err := <-done:
		t.Fatalf("en modo servicio el agente no debe salir ante un 401 (salio con %v)", err)
	default:
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("al detener el servicio debe salir sin error, got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("el agente inactivo no respondio a la senal de parada")
	}
	srv.mu.Lock()
	n := len(srv.metricsAuth)
	srv.mu.Unlock()
	if n != 1 {
		t.Errorf("requests got %d, want 1 (inactivo: no sigue golpeando al server con el token rechazado)", n)
	}
	if strings.Contains(logs.String(), testAgentToken) {
		t.Errorf("el token aparecio en los logs: %s", logs.String())
	}
}

// Tras re-enrolar (credenciales distintas en el archivo) el servicio retoma solo, sin reinicio.
func TestServiceModeResumesAfterReEnroll(t *testing.T) {
	t.Parallel()
	srv := newFakeServer(t)
	srv.metricsCode = http.StatusUnauthorized
	h := newHarness(t)
	h.app.idleLogEvery, h.app.idleRecheck = time.Hour, 10*time.Millisecond
	st := state.New(h.dir, h.app.logger)
	if err := st.Save(state.Credentials{HostID: "h", AgentToken: "token-viejo", ServerURL: srv.URL}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.app.execute(ctx, []string{"run", "--service"}) }()
	srv.waitMetrics(t) // primer intento con el token viejo: 401 y queda inactivo

	if err := st.Save(state.Credentials{HostID: "h", AgentToken: "token-nuevo", ServerURL: srv.URL}); err != nil {
		t.Fatal(err)
	}
	srv.waitMetrics(t) // retoma solo con el token nuevo
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.metricsAuth) < 2 || srv.metricsAuth[0] != "Bearer token-viejo" || srv.metricsAuth[1] != "Bearer token-nuevo" {
		t.Errorf("Authorization got %v, want primero el viejo y luego el nuevo", srv.metricsAuth)
	}
}

// Sin enroll, el servicio tampoco sale (un supervisor lo reiniciaria en bucle): espera a que lo enrolen.
func TestServiceModeNotEnrolledStaysIdle(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	logs := &syncBuffer{}
	h.app.logger = slog.New(slog.NewJSONHandler(logs, nil))
	h.app.idleLogEvery, h.app.idleRecheck = time.Hour, 10*time.Millisecond
	h.env["ARGOS_SERVER_URL"] = "http://127.0.0.1:1"

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.app.execute(ctx, []string{"run", "--service"}) }()
	waitFor(t, "aviso de que falta enrolar", func() bool { return strings.Contains(logs.String(), "no esta enrolado") })
	select {
	case err := <-done:
		t.Fatalf("no debe salir sin enroll en modo servicio (salio con %v)", err)
	default:
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("got %v, want nil al detener", err)
	}
}

// En primer plano, un 401 sigue terminando con ErrTokenRejected (salida 2).
func TestForegroundRejectedTokenStillExits(t *testing.T) {
	t.Parallel()
	srv := newFakeServer(t)
	srv.metricsCode = http.StatusUnauthorized
	h := newHarness(t)
	h.env["ARGOS_SERVER_URL"] = srv.URL
	h.env["ARGOS_AGENT_TOKEN"] = testAgentToken

	err := h.app.execute(context.Background(), []string{"run"})
	if !errors.Is(err, sender.ErrTokenRejected) {
		t.Fatalf("err got %v, want ErrTokenRejected", err)
	}
}

// richCollector suma discos y host_info al colector falso.
type richCollector struct{ fakeCollector }

func (richCollector) Disks(context.Context) ([]sender.DiskUsage, error) {
	return []sender.DiskUsage{{Mount: "/", UsedMB: 50000, TotalMB: 200000}}, nil
}

func (richCollector) HostInfo(context.Context) (sender.HostInfo, error) {
	return sender.HostInfo{BootTime: 1758400000, CPUModel: "cpu", CPUCores: 8, CPUThreads: 16, OSName: "os", IPs: []string{"192.168.1.25"}}, nil
}

// El primer reporte lleva discos y host_info (con la version del binario); sin colector que los mida, no van.
func TestFirstReportPayloadCarriesDisksAndHostInfoOnlyWhenMeasured(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		col  agent.Collector
		want bool
	}{
		{"colector completo", richCollector{}, true},
		{"colector solo con metricas", fakeCollector{}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := newFakeServer(t)
			h := newHarness(t)
			h.app.collector = tt.col
			h.env["ARGOS_SERVER_URL"] = srv.URL
			h.env["ARGOS_AGENT_TOKEN"] = testAgentToken

			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- h.app.execute(ctx, []string{"run"}) }()
			srv.waitMetrics(t)
			cancel()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			srv.mu.Lock()
			body := srv.metricsBody[0]
			srv.mu.Unlock()
			raw, _ := json.Marshal(body)

			_, hasDisks := body["disks"]
			_, hasInfo := body["host_info"]
			if hasDisks != tt.want || hasInfo != tt.want {
				t.Fatalf("disks=%v host_info=%v, want ambos %v: %s", hasDisks, hasInfo, tt.want, raw)
			}
			metrics, _ := body["metrics"].(map[string]any)
			for _, k := range []string{"swap", "load", "battery"} {
				if _, ok := metrics[k]; ok {
					t.Errorf("metrics.%s no debe aparecer si el colector no lo midio: %s", k, raw)
				}
			}
			if tt.want {
				info, _ := body["host_info"].(map[string]any)
				if info["agent_version"] != "test" {
					t.Errorf("host_info.agent_version got %v, want la version del binario (test)", info["agent_version"])
				}
			}
		})
	}
}

func TestInstallPrivilegedFlag(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()
	if err := h.app.execute(ctx, []string{"install"}); err != nil || h.svc.opts.Privileged {
		t.Errorf("sin --privileged: err %v Privileged %v, want false", err, h.svc.opts.Privileged)
	}
	if err := h.app.execute(ctx, []string{"install", "--privileged"}); err != nil || !h.svc.opts.Privileged {
		t.Errorf("con --privileged: err %v Privileged %v, want true", err, h.svc.opts.Privileged)
	}
}
