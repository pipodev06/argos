//go:build !linux && !darwin && !windows

package collector

// detectPrivilege: sin criterio pa este SO; no se reporta el campo.
func detectPrivilege() string { return "" }
