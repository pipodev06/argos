package collector

import (
	"bufio"
	"strconv"
	"strings"
)

// Niveles que se reportan en host_info.privilege.
const (
	privilegeElevated = "elevated"
	privilegeUser     = "user"
)

// capSysPtrace es el numero de CAP_SYS_PTRACE (linux/capability.h).
const capSysPtrace = 19

// privilegeLevel traduce "el proceso puede ver los procesos ajenos" al valor del protocolo.
func privilegeLevel(elevated bool) string {
	if elevated {
		return privilegeElevated
	}
	return privilegeUser
}

// linuxElevated dice si el proceso es root (euid 0) o tiene CAP_SYS_PTRACE efectiva segun el
// contenido de /proc/self/status. ok=false si no se pudo determinar (no se reporta nada).
func linuxElevated(euid int, status string) (elevated, ok bool) {
	if euid == 0 {
		return true, true
	}
	sc := bufio.NewScanner(strings.NewReader(status))
	for sc.Scan() {
		hex, found := strings.CutPrefix(sc.Text(), "CapEff:")
		if !found {
			continue
		}
		v, err := strconv.ParseUint(strings.TrimSpace(hex), 16, 64)
		if err != nil {
			return false, false
		}
		return v&(1<<capSysPtrace) != 0, true
	}
	return false, false
}
