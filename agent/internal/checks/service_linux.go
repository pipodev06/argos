//go:build linux

package checks

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// systemctlPaths: el binario se resuelve por ruta fija, no por PATH.
var systemctlPaths = []string{"/usr/bin/systemctl", "/bin/systemctl", "/usr/sbin/systemctl", "/sbin/systemctl"}

// runFunc ejecuta bin con argv fijo (sin shell) y devuelve su salida estandar.
func newServiceProber() ServiceProber {
	p := &systemdProber{run: runCommand}
	for _, c := range systemctlPaths {
		if fi, err := os.Stat(c); err == nil && fi.Mode().IsRegular() {
			p.bin = c
			break
		}
	}
	return p
}

func runCommand(ctx context.Context, bin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...) //nolint:gosec // bin de una lista fija; argv sin shell y nombre validado
	cmd.Env = []string{"LC_ALL=C"}
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ejecutando systemctl: %w", err)
	}
	return out, nil
}
