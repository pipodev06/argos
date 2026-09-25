// Comando argos-agent: agente de argos; reporta metricas del host al servidor central.
//
// Uso:
//
//	argos-agent [run]                                   reporta metricas en primer plano (subcomando por defecto)
//	argos-agent enroll --server <URL> --token-stdin     enrola el agente y guarda su token (el token llega por stdin)
//	argos-agent install [--ca-file ca.pem] [--privileged]  instala el servicio del SO (requiere administrador/root); --privileged: modo con mas visibilidad (opt-in)
//	argos-agent uninstall [--keep-state]                elimina el servicio y, por defecto, sus credenciales
//	argos-agent start | stop | status                   controlan el servicio
//
// Codigos de salida: 0 ok, 1 error, 2 el servidor rechazo el token (revocado o
// invalido: re-enrolar). El codigo 2 aplica solo en primer plano: como servicio, un
// token rechazado deja al agente inactivo con un log periodico (no sale, asi el
// supervisor no lo reinicia en bucle).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"argos/agent/internal/sender"
	"argos/agent/internal/service"
)

// version se inyecta en el build: go build -ldflags "-X main.version=1.2.3".
var version = "dev"

const (
	exitError         = 1
	exitTokenRejected = 2
)

func main() {
	os.Exit(realMain())
}

// realMain existe pa que los defer (senal, registro de eventos) corran antes de os.Exit.
func realMain() int {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a := &app{
		version: version,
		getenv:  os.Getenv,
		stdin:   os.Stdin,
		stdout:  os.Stdout,
		stderr:  os.Stderr,
		svc:     service.Manager{},
	}

	// Windows: si lo lanzo el gestor de servicios no hay consola; los logs van al registro de eventos.
	isService, err := service.IsService()
	if err != nil {
		fmt.Fprintln(os.Stderr, "aviso:", err) // se sigue en modo normal
	}
	if isService {
		a.serviceMode = true
		a.runAsService = service.RunService
		if l, closeLog, lerr := service.NewServiceLogger(); lerr == nil {
			logger = l
			defer func() {
				if cerr := closeLog(); cerr != nil {
					fmt.Fprintln(os.Stderr, "aviso: cerrando el registro de eventos:", cerr)
				}
			}()
		} else {
			fmt.Fprintln(os.Stderr, "aviso:", lerr) // sin registro de eventos: los logs van a stdout (se pierden)
		}
	}
	a.logger = logger

	err = a.execute(ctx, os.Args[1:])
	if err == nil {
		return 0
	}
	code := exitError
	if errors.Is(err, sender.ErrTokenRejected) {
		code = exitTokenRejected
	}
	if isService {
		// Sin consola: el error debe quedar en el registro de eventos.
		logger.Error("el servicio termino con error", "err", err)
	}
	fmt.Fprintln(os.Stderr, "error:", err)
	return code
}
