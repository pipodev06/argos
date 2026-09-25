//go:build windows

package collector

import "golang.org/x/sys/windows"

// detectPrivilege: token elevado (LocalSystem o administrador elevado) = "elevated"; la cuenta
// virtual del servicio o un usuario normal = "user".
func detectPrivilege() string {
	return privilegeLevel(windows.GetCurrentProcessToken().IsElevated())
}
