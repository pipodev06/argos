//go:build !linux && !windows

package checks

import "context"

type unsupportedProber struct{}

func newServiceProber() ServiceProber { return unsupportedProber{} }

// Probe: macOS y otros sistemas no soportan el chequeo `service`.
func (unsupportedProber) Probe(context.Context, string) (bool, error) {
	return false, ErrServiceUnsupported
}
