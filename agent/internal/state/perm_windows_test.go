//go:build windows

package state

import (
	"log/slog"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

const testServiceSID = "S-1-5-80-1111111111-2222222222-3333333333-1444444444-555555555"

func TestDirSDDL(t *testing.T) {
	t.Parallel()

	if got, want := dirSDDL("S-1-5-21-1", "", false), "D:P(A;OICI;FA;;;S-1-5-21-1)"; got != want {
		t.Errorf("modo usuario got %q, want %q", got, want)
	}
	svc := dirSDDL("", testServiceSID, false)
	if want := "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FR;;;" + testServiceSID + ")"; svc != want {
		t.Errorf("modo servicio got %q, want %q", svc, want)
	}
	// Buffer offline: la cuenta del servicio puede modificar (no cambiar la ACL).
	buf := dirSDDL("", testServiceSID, true)
	if want := "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;0x1301bf;;;" + testServiceSID + ")"; buf != want {
		t.Errorf("buffer got %q, want %q", buf, want)
	}
	if _, err := windows.SecurityDescriptorFromString(buf); err != nil {
		t.Errorf("SDDL del buffer invalido: %v", err)
	}
}

// sddl devuelve el descriptor de seguridad de path en SDDL (independiente del idioma de Windows).
func sddl(t *testing.T, path string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "acl.txt")
	cmd := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "icacls.exe"), path, "/save", out) //nolint:gosec // prueba: ruta fija de System32
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("icacls /save: %v: %s", err, b)
	}
	raw, err := os.ReadFile(out) //nolint:gosec // archivo temporal de la prueba
	if err != nil {
		t.Fatal(err)
	}
	// icacls /save escribe UTF-16LE con BOM: se descartan los bytes nulos.
	return strings.ReplaceAll(string(raw), "\x00", "")
}

// restoreAccess devuelve el acceso al usuario actual pa que t.TempDir pueda borrar el arbol.
func restoreAccess(t *testing.T, dir, uid string) {
	t.Helper()
	cmd := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "icacls.exe"), dir, "/grant", "*"+uid+":(OI)(CI)F", "/T") //nolint:gosec // prueba
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Logf("no se pudo restaurar el acceso (%v): %s", err, b)
	}
}

// Prueba real con icacls sobre un directorio temporal. No requiere elevacion: el
// propietario puede escribir la ACL.
func TestPrepareServiceACLWithRealIcacls(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "argos-agent")
	t.Cleanup(func() { restoreAccess(t, dir, u.Uid) })

	st := NewForService(dir, slog.New(slog.DiscardHandler), testServiceSID)
	if err := st.Prepare(); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	acl := sddl(t, dir)
	t.Logf("SDDL: %s", acl)
	if !strings.Contains(acl, "D:P") {
		t.Errorf("la herencia debe estar cortada (DACL protegida, D:P): %s", acl)
	}
	for _, want := range []string{";;;SY)", ";;;BA)", ";;;" + testServiceSID + ")"} {
		if !strings.Contains(acl, want) {
			t.Errorf("falta la ACE %q: %s", want, acl)
		}
	}
	if strings.Contains(acl, ";;;"+u.Uid+")") {
		t.Errorf("el usuario que creo el directorio no debe tener ACE propia en modo servicio: %s", acl)
	}
	// SYSTEM y Administrators: control total (FA); el servicio: lectura (FR = FILE_GENERIC_READ), nunca FA ni escritura.
	if !strings.Contains(acl, "(A;OICI;FA;;;SY)") || !strings.Contains(acl, "(A;OICI;FA;;;BA)") {
		t.Errorf("SYSTEM y Administrators deben tener control total heredable: %s", acl)
	}
	if !strings.Contains(acl, "(A;OICI;FR;;;"+testServiceSID+")") {
		t.Errorf("el servicio debe tener solo lectura heredable (FR = FILE_GENERIC_READ): %s", acl)
	}
}

func TestPrepareUserOnlyACLWithRealIcacls(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "argos-agent")
	t.Cleanup(func() { restoreAccess(t, dir, u.Uid) })

	if err := New(dir, slog.New(slog.DiscardHandler)).Prepare(); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	acl := sddl(t, dir)
	if !strings.Contains(acl, "D:P") || !strings.Contains(acl, ";;;"+u.Uid+")") {
		t.Errorf("modo usuario: DACL protegida con solo el SID propio: %s", acl)
	}
	if strings.Contains(acl, ";;;SY)") || strings.Contains(acl, ";;;BA)") {
		t.Errorf("modo usuario no debe conceder a SYSTEM/Administrators: %s", acl)
	}
	// El archivo guardado hereda la ACL del directorio y el guardado sigue funcionando.
	if err := New(dir, slog.New(slog.DiscardHandler)).Save(Credentials{HostID: "h", AgentToken: "t"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
}
