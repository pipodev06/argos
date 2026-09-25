//go:build windows

package inventory

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc/mgr"
)

// systemServices lista los servicios Win32 con derechos minimos: el SCM se abre con
// CONNECT|ENUMERATE_SERVICE y cada servicio con QUERY_CONFIG|QUERY_STATUS (mgr.OpenService pediria
// ALL_ACCESS y fallaria sin elevar). El estado sale de la enumeracion; el tipo de inicio, de
// la config de cada servicio. Nunca se lee la ruta binaria ni la cuenta de logon.
func systemServices(ctx context.Context) ([]Service, error) {
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT|windows.SC_MANAGER_ENUMERATE_SERVICE)
	if err != nil {
		return nil, fmt.Errorf("abriendo el SCM: %w", err)
	}
	defer func() { _ = windows.CloseServiceHandle(scm) }() // cierre best-effort de un handle de solo lectura

	entries, err := enumServices(scm)
	if err != nil {
		return nil, err
	}
	out := make([]Service, 0, len(entries))
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("listando servicios: %w", err)
		}
		name := windows.UTF16PtrToString(e.ServiceName)
		out = append(out, Service{
			Name:    name,
			Display: windows.UTF16PtrToString(e.DisplayName),
			State:   mapServiceState(e.ServiceStatusProcess.CurrentState),
			Start:   serviceStart(scm, name),
		})
	}
	return out, nil
}

func enumServices(scm windows.Handle) ([]windows.ENUM_SERVICE_STATUS_PROCESS, error) {
	var buf []byte
	var size uint32 // tamano de buf, en el tipo que pide la API
	for {
		var p *byte
		if size > 0 {
			p = &buf[0]
		}
		var needed, returned uint32
		err := windows.EnumServicesStatusEx(scm, windows.SC_ENUM_PROCESS_INFO, windows.SERVICE_WIN32,
			windows.SERVICE_STATE_ALL, p, size, &needed, &returned, nil, nil)
		if err == nil {
			if returned == 0 {
				return nil, nil
			}
			// La memoria de buf sigue viva mientras se use el slice devuelto (lo referencia).
			//nolint:gosec // mismo patron que mgr.ListServices: el buffer lo llena la API y sigue vivo por el slice
			return unsafe.Slice((*windows.ENUM_SERVICE_STATUS_PROCESS)(unsafe.Pointer(&buf[0])), int(returned)), nil
		}
		if !errors.Is(err, syscall.ERROR_MORE_DATA) || needed <= size {
			return nil, fmt.Errorf("enumerando servicios: %w", err)
		}
		buf, size = make([]byte, needed), needed
	}
}

// serviceStart devuelve el tipo de inicio; "other" si el servicio no deja consultar su config.
func serviceStart(scm windows.Handle, name string) string {
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return "other"
	}
	h, err := windows.OpenService(scm, namePtr, windows.SERVICE_QUERY_CONFIG|windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return "other"
	}
	s := &mgr.Service{Name: name, Handle: h}
	defer func() { _ = s.Close() }() // cierre best-effort de un handle de solo lectura
	cfg, err := s.Config()
	if err != nil {
		return "other"
	}
	return mapServiceStart(cfg.StartType)
}

func mapServiceState(s uint32) string {
	switch s {
	case windows.SERVICE_RUNNING:
		return "running"
	case windows.SERVICE_STOPPED:
		return "stopped"
	case windows.SERVICE_PAUSED:
		return "paused"
	default:
		return "other"
	}
}

func mapServiceStart(t uint32) string {
	switch t {
	case mgr.StartAutomatic:
		return "auto"
	case mgr.StartManual:
		return "manual"
	case mgr.StartDisabled:
		return "disabled"
	default:
		return "other"
	}
}

func servicesSource() func(ctx context.Context) ([]Service, error) { return systemServices }
