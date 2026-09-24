//go:build windows

package checks

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc/mgr"
)

type windowsProber struct{}

func newServiceProber() ServiceProber { return windowsProber{} }

// Probe abre el SCM con SC_MANAGER_CONNECT y el servicio con SERVICE_QUERY_STATUS:
// los derechos minimos (nunca ALL_ACCESS, que mgr.Connect/OpenService pedirian).
func (windowsProber) Probe(ctx context.Context, name string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("consultando servicio: %w", err)
	}
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return false, winErr(err)
	}
	defer func() { _ = windows.CloseServiceHandle(scm) }() // cierre best-effort de un handle de solo lectura

	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return false, ErrServiceNotFound
	}
	h, err := windows.OpenService(scm, namePtr, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return false, winErr(err)
	}
	s := &mgr.Service{Name: name, Handle: h}
	defer func() { _ = s.Close() }()
	st, err := s.Query()
	if err != nil {
		return false, winErr(err)
	}
	return windowsStateUp(uint32(st.State)), nil
}

func winErr(err error) error {
	var errno windows.Errno
	if errors.As(err, &errno) {
		return mapWindowsErr(uintptr(errno))
	}
	return ErrServiceUnsupported
}
