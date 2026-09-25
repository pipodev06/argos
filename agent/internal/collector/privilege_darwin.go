//go:build darwin

package collector

import "os"

// detectPrivilege: en macOS solo root (euid 0) ve los procesos de otros usuarios.
func detectPrivilege() string { return privilegeLevel(os.Geteuid() == 0) }
