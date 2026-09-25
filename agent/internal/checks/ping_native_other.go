//go:build !linux && !darwin && !windows

package checks

// newNativePinger: sin ICMP nativo en este SO; queda el ping del SO.
func newNativePinger() Pinger { return nil }
