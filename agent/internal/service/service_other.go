//go:build !windows && !linux && !darwin

package service

import (
	"context"
	"io"
	"path/filepath"
)

// StateDir: sin soporte de servicio en este SO; devuelve un valor inocuo.
func (Manager) StateDir() string { return filepath.Join("/var/lib", Name) }

// StateAccount: sin cuenta de servicio en este SO.
func (Manager) StateAccount() string { return "" }

// Installed: nunca hay servicio en este SO.
func (Manager) Installed() bool { return false }

// Install no esta soportado en este SO.
func (Manager) Install(context.Context, InstallOptions) error { return ErrUnsupported }

// Uninstall no esta soportado en este SO.
func (Manager) Uninstall(context.Context, bool, io.Writer) error { return ErrUnsupported }

// Start no esta soportado en este SO.
func (Manager) Start(context.Context) error { return ErrUnsupported }

// Stop no esta soportado en este SO.
func (Manager) Stop(context.Context) error { return ErrUnsupported }

// Status no esta soportado en este SO.
func (Manager) Status(context.Context) (string, error) { return "", ErrUnsupported }
