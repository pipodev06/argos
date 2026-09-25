package checks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const maxPingOutput = 4 << 10 // bytes de salida del ping del SO que se conservan

// rttRe extrae el RTT de la salida del ping del SO ("time=0.045 ms", "time<1ms",
// "tiempo=2ms"): solo el numero tras = o <, seguido de ms.
var rttRe = regexp.MustCompile(`[=<]\s*(\d{1,6}(?:[.,]\d{1,6})?)\s*ms?`)

// cmdRunner ejecuta un binario sin shell y devuelve su salida (acotada).
type cmdRunner func(ctx context.Context, path string, args []string) ([]byte, error)

// execPinger es el ultimo recurso: el `ping` del SO con argv fijo, sin shell.
type execPinger struct {
	goos   string
	binary string // ruta absoluta ("" = no hay ping en este host)
	run    cmdRunner
}

func newExecPinger() *execPinger {
	return &execPinger{goos: runtime.GOOS, binary: findPingBinary(runtime.GOOS), run: runCmd}
}

func findPingBinary(goos string) string {
	var cands []string
	switch goos {
	case "windows":
		if root := os.Getenv("SystemRoot"); root != "" {
			cands = []string{filepath.Join(root, "System32", "PING.EXE")}
		}
	case "darwin":
		cands = []string{"/sbin/ping"}
	default:
		cands = []string{"/bin/ping", "/usr/bin/ping", "/sbin/ping"}
	}
	for _, p := range cands {
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() { //nolint:gosec // p sale de una lista fija de rutas del SO
			return p
		}
	}
	return ""
}

// pingArgv es el argv COMPLETO (sin el nombre del binario) pa un unico echo. La IP la
// formatea este codigo desde un netip.Addr; el timeout va en segundos (Linux) o
// milisegundos (macOS, Windows), con un minimo de 1 s.
func pingArgv(goos string, ip netip.Addr, timeout time.Duration) []string {
	secs := strconv.Itoa(max(1, int((timeout+time.Second-1)/time.Second)))
	ms := strconv.FormatInt(max(1000, timeout.Milliseconds()), 10)
	ip = ip.Unmap()
	addr := ip.String()
	switch goos {
	case "windows":
		if ip.Is6() {
			return []string{"-6", "-n", "1", "-w", ms, addr}
		}
		return []string{"-n", "1", "-w", ms, addr}
	case "darwin":
		return []string{"-c", "1", "-W", ms, "--", addr}
	default:
		if ip.Is6() {
			return []string{"-6", "-c", "1", "-W", secs, "--", addr}
		}
		return []string{"-c", "1", "-W", secs, "--", addr}
	}
}

func (p *execPinger) Ping(ctx context.Context, ip netip.Addr) (time.Duration, error) {
	if p.binary == "" || !ip.IsValid() {
		return 0, ErrPingUnavailable
	}
	timeout := 2 * time.Second
	if dl, ok := ctx.Deadline(); ok {
		timeout = time.Until(dl)
	}
	binary := p.binary
	if p.goos == "darwin" && ip.Unmap().Is6() {
		binary = filepath.Join(filepath.Dir(binary), "ping6")
	}
	start := time.Now()
	out, err := p.run(ctx, binary, pingArgv(p.goos, ip, timeout))
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) || ctx.Err() != nil {
			return 0, ErrPingNoReply
		}
		return 0, fmt.Errorf("ejecutando ping: %w", ErrPingUnavailable)
	}
	if m := rttRe.FindSubmatch(out); m != nil {
		v, perr := strconv.ParseFloat(strings.Replace(string(m[1]), ",", ".", 1), 64)
		if perr == nil {
			return time.Duration(v * float64(time.Millisecond)), nil
		}
	}
	if p.goos == "windows" {
		return 0, ErrPingNoReply // exit 0 tambien con "host inalcanzable": sin RTT no hay respuesta
	}
	return time.Since(start), nil
}

func runCmd(ctx context.Context, path string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, path, args...) //nolint:gosec // path de una lista fija; argv armado por pingArgv desde un netip.Addr, sin shell
	var buf limitedBuffer
	cmd.Stdout = &buf
	cmd.WaitDelay = time.Second
	if runtime.GOOS != "windows" {
		cmd.Env = []string{"LC_ALL=C"}
	}
	if err := cmd.Run(); err != nil {
		return buf.b.Bytes(), fmt.Errorf("ping del SO: %w", err)
	}
	return buf.b.Bytes(), nil
}

// limitedBuffer descarta lo que pase de maxPingOutput.
type limitedBuffer struct{ b bytes.Buffer }

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := maxPingOutput - l.b.Len(); room > 0 {
		l.b.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}
