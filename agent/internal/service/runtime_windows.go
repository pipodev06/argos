//go:build windows

package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
)

const (
	// stopWait es cuanto se espera a que el agente termine tras una orden de parada del SCM.
	stopWait = 25 * time.Second
	// exitCodeFailure es el codigo especifico del servicio cuando termina con error (dispara la recuperacion).
	exitCodeFailure = 1
)

// IsService dice si el proceso lo lanzo el administrador de servicios de Windows.
func IsService() (bool, error) {
	ok, err := svc.IsWindowsService()
	if err != nil {
		return false, fmt.Errorf("detectando el modo servicio: %w", err)
	}
	return ok, nil
}

// RunService ejecuta run bajo el administrador de servicios: informa Running al
// SCM, cancela el contexto de run cuando llega Stop o Shutdown y espera a que
// termine. Bloquea hasta que el servicio se detiene.
func RunService(run func(context.Context) error) error {
	if err := svc.Run(Name, &handler{run: run}); err != nil {
		return fmt.Errorf("ejecutando el servicio: %w", err)
	}
	return nil
}

type handler struct {
	run func(context.Context) error
}

// Execute implementa svc.Handler.
func (h *handler) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (specificExitCode bool, exitCode uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	status <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.run(ctx) }() // dueno: este Execute; termina al cancelar ctx (o si run falla)

	status <- svc.Status{State: svc.Running, Accepts: accepted}
	for {
		select {
		case req := <-requests:
			switch req.Cmd {
			case svc.Interrogate:
				status <- req.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending, WaitHint: uint32(stopWait / time.Millisecond)}
				cancel()
				select {
				case <-done:
				case <-time.After(stopWait):
				}
				return false, 0
			}
		case err := <-done:
			// run termino solo: con error se sale con un codigo distinto de cero pa que aplique la recuperacion.
			if err != nil {
				return true, exitCodeFailure
			}
			return false, 0
		}
	}
}

// NewServiceLogger devuelve un logger que escribe en el registro de eventos de
// Windows (origen argos-agent, registrado por `install`) y la funcion pa cerrarlo.
func NewServiceLogger() (*slog.Logger, func() error, error) {
	l, err := eventlog.Open(Name)
	if err != nil {
		return nil, nil, fmt.Errorf("abriendo el registro de eventos: %w", err)
	}
	return slog.New(NewEventLogHandler(l, slog.LevelInfo)), l.Close, nil
}
