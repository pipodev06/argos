//go:build windows

package checks

import (
	"errors"
	"testing"
)

// Consultas de solo lectura (SC_MANAGER_CONNECT + SERVICE_QUERY_STATUS) a servicios del sistema: no instala ni toca nada.
func TestWindowsProberReal(t *testing.T) {
	p := newServiceProber()
	if _, err := p.Probe(t.Context(), "argos-servicio-que-no-existe"); !errors.Is(err, ErrServiceNotFound) {
		t.Errorf("inexistente: err = %v, want ErrServiceNotFound", err)
	}
	up, err := p.Probe(t.Context(), "RpcSs")
	if errors.Is(err, ErrServiceDenied) {
		t.Skip("sin acceso de consulta a RpcSs")
	}
	if err != nil || !up {
		t.Errorf("RpcSs: up=%v err=%v, want activo", up, err)
	}
}
