//go:build !windows

package inventory

import "context"

// servicesSource es nil fuera de Windows: los servicios de Linux quedan pa v1.1 y macOS no aplica.
func servicesSource() func(ctx context.Context) ([]Service, error) { return nil }
