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

// RenderSystemdUnit genera la unit de systemd pa el binario instalado en execPath.
func RenderSystemdUnit(execPath string) (string, error) {
	if err := safePath(execPath); err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := systemdUnit.Execute(&b, struct{ ExecPath string }{execPath}); err != nil {
		return "", fmt.Errorf("generando la unit de systemd: %w", err)
	}
	// La plantilla embebida puede traer CRLF si se edito en Windows: systemd exige LF.
	return strings.ReplaceAll(b.String(), "\r\n", "\n"), nil
}
