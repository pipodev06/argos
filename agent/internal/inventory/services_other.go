//go:build !windows && !linux

package inventory

import "context"

// servicesSource es nil en SO sin implementacion (macOS): el modulo se omite con aviso.
func servicesSource() func(ctx context.Context) ([]Service, error) { return nil }
