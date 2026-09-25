package inventory

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"strings"
)

// Sin sufijo de SO a proposito: la logica de systemd se prueba en cualquier plataforma;
// solo el runner real y la deteccion de systemd viven en services_linux.go.

// maxSystemctlOutput acota lo que se lee de cada llamada a systemctl (1024 servicios caben de sobra).
const maxSystemctlOutput = 2 << 20

// systemctlRunner ejecuta bin con argv fijo y devuelve como mucho limit bytes de stdout.
type systemctlRunner func(ctx context.Context, bin string, limit int64, args ...string) ([]byte, error)

// Argv fijos: un test falla si alguien añade un argumento.
var (
	listUnitsArgs     = []string{"list-units", "--type=service", "--all", "--no-legend", "--plain", "--no-pager"}
	listUnitFilesArgs = []string{"list-unit-files", "--type=service", "--no-legend", "--plain", "--no-pager"}
)

// systemdServices une list-units (estado, descripcion) y list-unit-files (tipo de inicio).
// Como en Windows, se listan todos los servicios: los que solo aparecen como archivo de
// unidad (no cargados) se reportan "stopped" y sin descripcion. Se omiten las plantillas (foo@.service).
func systemdServices(ctx context.Context, run systemctlRunner, bin string) ([]Service, error) {
	unitsOut, err := run(ctx, bin, maxSystemctlOutput, listUnitsArgs...)
	if err != nil {
		return nil, fmt.Errorf("listando unidades: %w", err)
	}
	filesOut, err := run(ctx, bin, maxSystemctlOutput, listUnitFilesArgs...)
	if err != nil {
		return nil, fmt.Errorf("listando archivos de unidad: %w", err)
	}
	starts := parseUnitFiles(filesOut)
	seen := map[string]bool{}
	var out []Service
	for _, f := range fieldLines(unitsOut) {
		if len(f) < 4 {
			continue
		}
		name, ok := serviceName(f[0])
		if !ok {
			continue
		}
		seen[name] = true
		start := "other"
		if s, found := starts[name]; found {
			start = s
		}
		out = append(out, Service{
			Name:    name,
			Display: strings.Join(f[4:], " "),
			State:   mapSystemdState(f[1], f[2], f[3]),
			Start:   start,
		})
	}
	for name, start := range starts {
		if !seen[name] {
			out = append(out, Service{Name: name, State: "stopped", Start: start})
		}
	}
	return out, nil
}

// fieldLines parte la salida en lineas de campos; quita el marcador de estado (●, ○, *) que
// systemctl antepone a las unidades con problemas y descarta lo que pase del tope.
func fieldLines(out []byte) [][]string {
	if len(out) > maxSystemctlOutput {
		out = out[:maxSystemctlOutput]
	}
	var lines [][]string
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 4096), 64<<10)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) > 0 && (f[0] == "●" || f[0] == "○" || f[0] == "*") {
			f = f[1:]
		}
		if len(f) > 0 {
			lines = append(lines, f)
		}
	}
	return lines
}

func parseUnitFiles(out []byte) map[string]string {
	m := map[string]string{}
	for _, f := range fieldLines(out) {
		if len(f) < 2 {
			continue
		}
		if name, ok := serviceName(f[0]); ok {
			m[name] = mapSystemdStart(f[1])
		}
	}
	return m
}

// serviceName quita el sufijo .service (igual que el nombre plano de Windows) y descarta plantillas.
func serviceName(unit string) (string, bool) {
	name, ok := strings.CutSuffix(unit, ".service")
	if !ok || name == "" || strings.HasSuffix(name, "@") {
		return "", false
	}
	return name, true
}

// mapSystemdState: active+running -> running; inactive y failed -> stopped; el resto
// (active exited/waiting, activating, reloading, not-found...) -> other.
func mapSystemdState(load, active, sub string) string {
	switch {
	case load == "not-found":
		return "other"
	case active == "active" && sub == "running":
		return "running"
	case active == "inactive" || active == "failed":
		return "stopped"
	default:
		return "other"
	}
}

// mapSystemdStart: enabled -> auto; disabled y masked -> disabled; static/indirect/generated
// (solo arrancan por dependencia o a mano) -> manual; el resto -> other.
func mapSystemdStart(state string) string {
	switch state {
	case "enabled", "enabled-runtime", "alias":
		return "auto"
	case "disabled", "masked", "masked-runtime":
		return "disabled"
	case "static", "indirect", "generated", "transient":
		return "manual"
	default:
		return "other"
	}
}
