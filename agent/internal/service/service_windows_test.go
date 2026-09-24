//go:build windows

package service

import (
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc/mgr"
)

// Construye la configuracion del servicio sin registrarlo (no requiere elevacion).
func TestServiceConfig(t *testing.T) {
	t.Parallel()

	c := serviceConfig()
	if c.ServiceStartName != `NT SERVICE\argos-agent` {
		t.Errorf("cuenta got %q, want la cuenta virtual NT SERVICE\\argos-agent", c.ServiceStartName)
	}
	if c.Password != "" {
		t.Error("una cuenta virtual no lleva contrasena (CreateService exige NULL)")
	}
	if c.StartType != mgr.StartAutomatic || !c.DelayedAutoStart {
		t.Errorf("inicio got type=%d delayed=%v, want automatico retardado", c.StartType, c.DelayedAutoStart)
	}
	if c.ServiceType != windows.SERVICE_WIN32_OWN_PROCESS {
		t.Errorf("tipo got %d, want proceso propio", c.ServiceType)
	}
	if c.SidType != windows.SERVICE_SID_TYPE_UNRESTRICTED {
		t.Errorf("SidType got %d, want unrestricted (pa que la ACL por SID del servicio aplique)", c.SidType)
	}
	if c.DisplayName == "" || c.Description == "" {
		t.Error("faltan nombre visible o descripcion")
	}
	// Nunca LocalSystem: sin cuenta explicita CreateService usaria LocalSystem.
	if strings.EqualFold(c.ServiceStartName, "LocalSystem") || c.ServiceStartName == "" {
		t.Errorf("no debe correr como LocalSystem: %q", c.ServiceStartName)
	}
}

func TestQuotedBinaryPath(t *testing.T) {
	t.Parallel()

	got, err := quotedBinaryPath(`C:\Program Files\argos-agent\argos-agent.exe`)
	if err != nil || got != `"C:\Program Files\argos-agent\argos-agent.exe"` {
		t.Errorf("got %q err %v, want la ruta entre comillas", got, err)
	}
	if _, err := quotedBinaryPath(`C:\argos\argos-agent.exe`); err == nil {
		t.Error("una ruta sin espacios queda sin comillas: debe rechazarse")
	}
}

func TestInstallExeIsUnderProgramFiles(t *testing.T) {
	t.Parallel()

	exe, err := installExe()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(exe, `\argos-agent\argos-agent.exe`) || !strings.Contains(strings.ToLower(exe), "program files") {
		t.Errorf("installExe got %q, want %%ProgramFiles%%\\argos-agent\\argos-agent.exe", exe)
	}
	if got := (Manager{}).StateDir(); !strings.HasSuffix(got, `\argos-agent`) || !strings.Contains(strings.ToLower(got), "programdata") {
		t.Errorf("StateDir got %q, want %%ProgramData%%\\argos-agent", got)
	}
}

func TestRecoveryActionsRestartWithBackoff(t *testing.T) {
	t.Parallel()

	actions, reset := recoveryActions()
	if len(actions) != 3 || reset != 86400 {
		t.Fatalf("got %d acciones reset=%d", len(actions), reset)
	}
	prev := time.Duration(0)
	for _, a := range actions {
		if a.Type != mgr.ServiceRestart || a.Delay <= prev {
			t.Errorf("accion %+v: debe ser reinicio con espera creciente (previa %s)", a, prev)
		}
		prev = a.Delay
	}
}

// Sin elevacion, las operaciones de administracion deben abortar con el mensaje claro (no se auto-eleva).
func TestAdminOperationsRefuseWithoutElevation(t *testing.T) {
	t.Parallel()

	if windows.GetCurrentProcessToken().IsElevated() {
		t.Skip("la consola de prueba esta elevada: no se prueba el rechazo (y nunca se instala un servicio real)")
	}
	m := Manager{}
	if err := m.Install(t.Context(), InstallOptions{}); err != ErrNotPrivileged { //nolint:errorlint // se compara el centinela exacto a proposito
		t.Errorf("Install got %v, want ErrNotPrivileged", err)
	}
	if err := m.Start(t.Context()); err != ErrNotPrivileged { //nolint:errorlint // idem
		t.Errorf("Start got %v, want ErrNotPrivileged", err)
	}
	if err := m.Uninstall(t.Context(), true, nil); err != ErrNotPrivileged { //nolint:errorlint // idem
		t.Errorf("Uninstall got %v, want ErrNotPrivileged", err)
	}
}
