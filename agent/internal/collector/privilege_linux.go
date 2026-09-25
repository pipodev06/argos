//go:build linux

package collector

import "os"

// detectPrivilege: root o CAP_SYS_PTRACE efectiva = "elevated"; vacio si no se pudo leer.
func detectPrivilege() string {
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return ""
	}
	elevated, ok := linuxElevated(os.Geteuid(), string(status))
	if !ok {
		return ""
	}
	return privilegeLevel(elevated)
}
