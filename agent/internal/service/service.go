// Package service instala y controla el agente como servicio nativo del SO:
// servicio de Windows (cuenta virtual NT SERVICE\argos-agent), unit de systemd en
// Linux y LaunchDaemon en macOS. Sin dependencias fuera de la biblioteca estandar
// y golang.org/x/sys.
package service

import (
	"crypto/sha1" //nolint:gosec // no es criptografia: asi Windows deriva el SID de un servicio a partir de su nombre
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"unicode/utf16"

	"argos/agent/internal/state"
)

const (
	// Name es el nombre del servicio en systemd y en el SCM de Windows, y del usuario de sistema en Linux.
	Name = "argos-agent"
	// DisplayName y Description se muestran en el administrador de servicios de Windows.
	DisplayName = "Argos Agent"
	Description = "Agente de monitoreo Argos: reporta metricas del equipo al servidor central."

	// maxCABytes acota la CA que se copia (un PEM real ocupa unos KiB).
	maxCABytes = 1 << 20
)

// ErrNotPrivileged: la operacion exige administrador/root. No se auto-eleva.
var ErrNotPrivileged = errors.New("esta operacion requiere privilegios de administrador (Windows: consola elevada) o root (Linux/macOS: usa sudo); el agente no se auto-eleva")

// ErrUnsupported: este sistema operativo no tiene soporte de servicio.
var ErrUnsupported = errors.New("la instalacion como servicio no esta soportada en este sistema operativo")

// ErrAlreadyInstalled: el servicio ya existe; hay que desinstalarlo antes.
var ErrAlreadyInstalled = errors.New("el servicio ya esta instalado: ejecuta primero `argos-agent uninstall --keep-state` y vuelve a instalar")

// InstallOptions son las entradas de Install.
type InstallOptions struct {
	// CAFile es la CA privada (PEM) del servidor; se copia al directorio de estado como ca.pem.
	CAFile string
	// Privileged (--privileged) da al agente la visibilidad completa con el minimo privilegio del SO
	// (Linux: capacidades + sudoers acotado; Windows: LocalSystem; macOS: root). Por defecto, sin privilegios.
	Privileged bool
	// Out recibe los avisos de progreso.
	Out io.Writer
	// Logger recibe los avisos del almacen de estado (p. ej. si no se pudo endurecer un permiso).
	Logger *slog.Logger
}

// Manager instala y controla el servicio del SO. Sus metodos (Install, Uninstall,
// Start, Stop, Status, Installed, StateDir, StateAccount) se implementan por SO.
type Manager struct{}

// prepareServiceState deja listo el directorio de estado del servicio y su subdirectorio del buffer
// offline (el unico donde escribe la cuenta del servicio). Se llama con privilegios (install).
func prepareServiceState(dir, account string, logger *slog.Logger) (*state.Store, error) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	st := state.NewForService(dir, logger, account)
	if err := st.Prepare(); err != nil {
		return nil, err
	}
	if err := st.Buffer().Prepare(); err != nil {
		return nil, err
	}
	return st, nil
}

// LoadCA lee y valida una CA privada en PEM antes de copiarla: acotada en tamano y
// con al menos un certificado valido (un archivo equivocado se rechaza al instalar,
// no cuando el agente ya no logra conectarse).
func LoadCA(path string) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // ruta elegida por el operador (--ca-file / ARGOS_CA_FILE)
	if err != nil {
		return nil, fmt.Errorf("abriendo la CA: %w", err)
	}
	defer func() { _ = f.Close() }() // solo lectura
	pem, err := io.ReadAll(io.LimitReader(f, maxCABytes+1))
	if err != nil {
		return nil, fmt.Errorf("leyendo la CA: %w", err)
	}
	if len(pem) > maxCABytes {
		return nil, fmt.Errorf("la CA %s pesa mas de %d bytes", path, maxCABytes)
	}
	if !x509.NewCertPool().AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("la CA %s no contiene ningun certificado PEM valido", path)
	}
	return pem, nil
}

// ServiceSID devuelve el SID que Windows deriva del nombre de un servicio
// (S-1-5-80-<5 subautoridades>): SHA-1 del nombre en mayusculas, UTF-16LE, leido
// como cinco enteros de 32 bits little-endian. Es el SID de la cuenta virtual
// NT SERVICE\<nombre>; se calcula sin resolver nombres pa poder fijar la ACL
// aunque el servicio aun no exista.
func ServiceSID(name string) string {
	units := utf16.Encode([]rune(strings.ToUpper(name)))
	raw := make([]byte, 0, len(units)*2)
	for _, u := range units {
		raw = binary.LittleEndian.AppendUint16(raw, u)
	}
	sum := sha1.Sum(raw) //nolint:gosec // ver el import
	var b strings.Builder
	b.WriteString("S-1-5-80")
	for i := range 5 {
		fmt.Fprintf(&b, "-%d", binary.LittleEndian.Uint32(sum[i*4:]))
	}
	return b.String()
}

func printf(out io.Writer, format string, args ...any) {
	if out != nil {
		_, _ = fmt.Fprintf(out, format, args...) // progreso informativo: un fallo al escribirlo no debe abortar la instalacion
	}
}
