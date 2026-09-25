package service

import (
	"bytes"
	_ "embed" // plantilla de la unit
	"fmt"
	"strings"
	"text/template"
)

//go:embed argos-agent.service.tmpl
var systemdTemplate string

var systemdUnit = template.Must(template.New("unit").Parse(systemdTemplate))

// safePath acepta solo rutas absolutas con caracteres inocuos pa systemd y launchd
// (sin espacios, comillas, %, $ ni saltos de linea que alteren la unit o el plist).
func safePath(p string) error {
	if !strings.HasPrefix(p, "/") {
		return fmt.Errorf("la ruta del ejecutable debe ser absoluta: %q", p)
	}
	for _, r := range p {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._-", r)
		if !ok {
			return fmt.Errorf("la ruta del ejecutable tiene caracteres no permitidos: %q", p)
		}
	}
	return nil
}

// Capacidades del modo privilegiado (install --privileged), solo pa el usuario argos-agent:
//   - CAP_DAC_READ_SEARCH y CAP_SYS_PTRACE (ambientales): leer /proc/<pid>/fd y /proc/<pid>/exe
//     de procesos de otros usuarios (dueno de cada puerto). Ninguna directiva de /proc choca: la
//     unit no usa ProtectProc ni ProcSubset, y PrivateDevices/Protect* no afectan a /proc.
//   - Solo si se instala el sudoers (pa `dnf`/`zypper` sin root): sudo es setuid, asi que
//     NoNewPrivileges pasa a false y el bounding set suma CAP_SETUID, CAP_SETGID y CAP_AUDIT_WRITE
//     (sudo no arranca sin ellas; NO son ambientales: el agente no las tiene), y el filtro de
//     syscalls suma @privileged (setuid/setgid). El resto del bounding set sigue cerrado
//     (sin CAP_SYS_ADMIN, CAP_NET_RAW, etc.), asi que un root via sudo queda igual de acotado.
const (
	capsAmbient  = "CAP_DAC_READ_SEARCH CAP_SYS_PTRACE"
	capsSudoOnly = "CAP_SETUID CAP_SETGID CAP_AUDIT_WRITE"
)

// UnitOptions son las variantes de la unit. El valor cero es la unit sin privilegios.
type UnitOptions struct {
	// Privileged agrega CAP_DAC_READ_SEARCH y CAP_SYS_PTRACE.
	Privileged bool
	// Sudo (solo con Privileged) deja que el modulo updates use sudo con el sudoers instalado.
	Sudo bool
}

// RenderSystemdUnit genera la unit de systemd sin privilegios pa el binario instalado en execPath.
func RenderSystemdUnit(execPath string) (string, error) {
	return RenderSystemdUnitOpts(execPath, UnitOptions{})
}

// RenderSystemdUnitOpts genera la unit de systemd pa execPath con las opciones dadas.
func RenderSystemdUnitOpts(execPath string, o UnitOptions) (string, error) {
	if err := safePath(execPath); err != nil {
		return "", err
	}
	v := struct {
		ExecPath, NoNewPrivileges, BoundingCaps, AmbientCaps, SyscallFilter string
		Sudo                                                                bool
	}{ExecPath: execPath, NoNewPrivileges: "true", SyscallFilter: "@system-service"}
	if o.Privileged {
		v.BoundingCaps, v.AmbientCaps = capsAmbient, capsAmbient
		if o.Sudo {
			v.Sudo = true
			v.NoNewPrivileges = "false"
			v.BoundingCaps = capsAmbient + " " + capsSudoOnly
			v.SyscallFilter = "@system-service @privileged"
		}
	}
	var b bytes.Buffer
	if err := systemdUnit.Execute(&b, v); err != nil {
		return "", fmt.Errorf("generando la unit de systemd: %w", err)
	}
	// La plantilla embebida puede traer CRLF si se edito en Windows: systemd exige LF.
	return strings.ReplaceAll(b.String(), "\r\n", "\n"), nil
}
