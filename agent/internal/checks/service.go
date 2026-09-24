package checks

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Errores de dominio del chequeo `service`.
var (
	ErrServiceNotFound    = errors.New("servicio no encontrado")
	ErrServiceDenied      = errors.New("acceso denegado al servicio")
	ErrServiceUnsupported = errors.New("chequeo de servicios no soportado en este sistema")
)

// ServiceProber consulta si un servicio del SO esta activo. up=false sin error = detenido.
type ServiceProber interface {
	Probe(ctx context.Context, name string) (up bool, err error)
}

// Codigos de Windows que interesan (winerror.h, svc.State).
const (
	winErrAccessDenied    = 5
	winErrServiceNotExist = 1060
	winErrInvalidName     = 123
	winErrServiceNameBad  = 1078
	winStateRunning       = 4
)

// windowsStateUp: solo SERVICE_RUNNING cuenta como activo.
func windowsStateUp(state uint32) bool { return state == winStateRunning }

// mapWindowsErr traduce un codigo de error de Windows al error de dominio.
func mapWindowsErr(code uintptr) error {
	switch code {
	case winErrAccessDenied:
		return ErrServiceDenied
	case winErrServiceNotExist, winErrInvalidName, winErrServiceNameBad:
		return ErrServiceNotFound
	default:
		return ErrServiceUnsupported
	}
}

// systemdState es lo que interesa de `systemctl show`.
type systemdState struct{ load, active string }

// parseSystemctlShow lee la salida KEY=value de `systemctl show -p LoadState -p ActiveState -p SubState`.
// Se pide con clave (sin --value): el orden de los valores sin clave no esta garantizado.
func parseSystemctlShow(out string) (systemdState, bool) {
	var st systemdState
	for line := range strings.SplitSeq(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch k {
		case "LoadState":
			st.load = v
		case "ActiveState":
			st.active = v
		}
	}
	return st, st.active != ""
}

// systemdUp: active (y reloading, que sigue sirviendo) cuenta como activo.
func systemdUp(st systemdState) (bool, error) {
	if st.load == "not-found" {
		return false, ErrServiceNotFound
	}
	return st.active == "active" || st.active == "reloading", nil
}

type runFunc func(ctx context.Context, bin string, args ...string) ([]byte, error)

type systemdProber struct {
	bin string
	run runFunc
}

func (p *systemdProber) Probe(ctx context.Context, name string) (bool, error) {
	if p.bin == "" {
		return false, ErrServiceUnsupported
	}
	if !validServiceName(name) {
		return false, ErrServiceNotFound
	}
	out, err := p.run(ctx, p.bin, "show", "--no-pager", "-p", "LoadState", "-p", "ActiveState", "-p", "SubState", "--", name)
	if err != nil {
		if ctx.Err() != nil {
			return false, fmt.Errorf("consultando systemd: %w", ctx.Err())
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return false, ErrServiceDenied // systemctl corrio pero no pudo consultar (bus inaccesible, etc.)
		}
		return false, ErrServiceUnsupported
	}
	st, ok := parseSystemctlShow(string(out))
	if !ok {
		return false, ErrServiceUnsupported
	}
	return systemdUp(st)
}
