//go:build linux

package inventory

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

// Sufijo _linux intencional: services_other.go cubre el resto (!windows && !linux).

// systemctlPaths: el binario se resuelve por ruta fija, no por PATH.
var systemctlPaths = []string{"/usr/bin/systemctl", "/bin/systemctl", "/usr/sbin/systemctl", "/sbin/systemctl"}

const systemctlTimeout = 15 * time.Second

// servicesSource devuelve nil (modulo omitido con aviso) si no hay systemd ni systemctl.
func servicesSource() func(ctx context.Context) ([]Service, error) {
	if fi, err := os.Stat("/run/systemd/system"); err != nil || !fi.IsDir() {
		return nil
	}
	for _, p := range systemctlPaths {
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			return func(ctx context.Context) ([]Service, error) { return systemdServices(ctx, runSystemctl, p) }
		}
	}
	return nil
}

// runSystemctl ejecuta bin sin shell, sin privilegios, con entorno minimo, timeout y salida acotada.
func runSystemctl(ctx context.Context, bin string, limit int64, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, systemctlTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...) //nolint:gosec // bin de una lista fija; argv fijo sin shell
	cmd.Env = []string{"LC_ALL=C"}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("preparando systemctl: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("iniciando systemctl: %w", err)
	}
	out, readErr := io.ReadAll(io.LimitReader(stdout, limit))
	if readErr == nil && int64(len(out)) >= limit {
		cancel() // salida mas grande que el tope: se corta el proceso
	}
	if err := cmd.Wait(); err != nil && ctx.Err() == nil {
		return nil, fmt.Errorf("ejecutando systemctl: %w", err)
	}
	if readErr != nil {
		return nil, fmt.Errorf("leyendo systemctl: %w", readErr)
	}
	return out, nil
}
