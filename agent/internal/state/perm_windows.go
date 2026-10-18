//go:build windows

package state

import (
	"fmt"
	"os/user"

	"golang.org/x/sys/windows"
)

const (
	sidSystem         = "SY" // NT AUTHORITY\SYSTEM
	sidAdministrators = "BA" // BUILTIN\Administrators
	// rightModify es el permiso "Modificar" de Windows: leer, escribir, ejecutar y borrar (sin cambiar la ACL).
	rightModify = "0x1301bf"
)

// dirSDDL arma el descriptor de seguridad del directorio de estado. La DACL es
// protegida (sin herencia del padre) y sus ACE se heredan a los archivos nuevos
// (OICI). Sin serviceSID: control total solo pa el SID del usuario actual (uso
// normal). Con serviceSID (modo servicio): control total pa SYSTEM y
// Administrators y, pa la cuenta del servicio, solo LECTURA (credenciales: no hay
// rotacion de token que la obligue a escribir) o MODIFICAR si writable (buffer offline).
func dirSDDL(userSID, serviceSID string, writable bool) string {
	if serviceSID == "" {
		return "D:P(A;OICI;FA;;;" + userSID + ")"
	}
	right := "FR"
	if writable {
		right = rightModify
	}
	return "D:P(A;OICI;FA;;;" + sidSystem + ")(A;OICI;FA;;;" + sidAdministrators + ")(A;OICI;" + right + ";;;" + serviceSID + ")"
}

// restrictDir aplica la ACL con la API nativa (SetNamedSecurityInfo): acepta un
// SID aunque no se pueda resolver a un nombre y es una sola operacion atomica.
// serviceAccount es el SID de la cuenta del servicio (vacio = uso normal); writable, ver dirSDDL.
func restrictDir(dir, serviceAccount string, writable bool) error {
	userSID := ""
	if serviceAccount == "" {
		u, err := user.Current()
		if err != nil {
			return fmt.Errorf("usuario actual: %w", err)
		}
		userSID = u.Uid // en Windows Uid es el SID
	}
	sd, err := windows.SecurityDescriptorFromString(dirSDDL(userSID, serviceAccount, writable))
	if err != nil {
		return fmt.Errorf("descriptor de seguridad: %w", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("leyendo la DACL: %w", err)
	}
	err = windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
	if err != nil {
		return fmt.Errorf("aplicando la ACL: %w", err)
	}
	return nil
}

// restrictFile no hace nada: el archivo temporal se crea dentro del directorio
// ya restringido y hereda su ACL.
func restrictFile(string, string) error { return nil }
