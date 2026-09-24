//go:build !windows

package service

import (
	"context"
	"log/slog"
)

// IsService dice si el proceso corre bajo el gestor de servicios de Windows. En
// Linux y macOS el gestor (systemd, launchd) lanza `argos-agent run --service`
// como un proceso normal, asi que aqui siempre es false.
func IsService() (bool, error) { return false, nil }

// RunService solo aplica a Windows.
func RunService(func(context.Context) error) error { return ErrUnsupported }

// NewServiceLogger solo aplica a Windows (registro de eventos).
func NewServiceLogger() (*slog.Logger, func() error, error) { return nil, nil, ErrUnsupported }
