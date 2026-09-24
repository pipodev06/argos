//go:build windows

package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"

	"argos/agent/internal/state"
)

const (
	// serviceAccount es la cuenta virtual del servicio: sin contrasena y sin los
	// privilegios de LocalSystem. Windows la crea sola al arrancar el servicio.
	serviceAccount = `NT SERVICE\` + Name

	stopTimeout = 30 * time.Second
)

// knownFolder resuelve una carpeta conocida (Program Files, ProgramData) sin depender de variables de entorno.
func knownFolder(id *windows.KNOWNFOLDERID) (string, error) {
	p, err := windows.KnownFolderPath(id, windows.KF_FLAG_DEFAULT)
	if err != nil {
		return "", fmt.Errorf("resolviendo carpeta del sistema: %w", err)
	}
	return p, nil
}

// installExe es %ProgramFiles%\argos-agent\argos-agent.exe (Program Files solo lo escriben administradores).
func installExe() (string, error) {
	pf, err := knownFolder(windows.FOLDERID_ProgramFiles)
	if err != nil {
		return "", err
	}
	return filepath.Join(pf, Name, Name+".exe"), nil
}

// StateDir es %ProgramData%\argos-agent.
func (Manager) StateDir() string {
	pd, err := knownFolder(windows.FOLDERID_ProgramData)
	if err != nil {
		pd = `C:\ProgramData` // ubicacion estandar si la consulta falla
	}
	return filepath.Join(pd, Name)
}

// StateAccount es el SID de NT SERVICE\argos-agent (ver state.NewForService).
func (Manager) StateAccount() string { return ServiceSID(Name) }

// Installed dice si el binario del servicio esta instalado (no requiere privilegios).
func (Manager) Installed() bool {
	exe, err := installExe()
	if err != nil {
		return false
	}
	_, err = os.Stat(exe)
	return err == nil
}

func requireElevated() error {
	if !windows.GetCurrentProcessToken().IsElevated() {
		return ErrNotPrivileged
	}
	return nil
}

// quotedBinaryPath es la ruta como la guarda el SCM: entrecomillada. Una ruta con
// espacios sin comillas permite escalar privilegios; syscall.EscapeArg (el que usa
// mgr.CreateService) solo comilla si hay espacios, por eso se exige.
func quotedBinaryPath(exe string) (string, error) {
	q := syscall.EscapeArg(exe)
	if len(q) < 2 || q[0] != '"' {
		return "", fmt.Errorf("la ruta del ejecutable %q no tiene espacios y quedaria sin comillas en el servicio; instala en una ruta con espacios (Program Files)", exe)
	}
	return q, nil
}

// serviceConfig es la configuracion del servicio (sin registrarlo): inicio
// automatico retardado y cuenta virtual NT SERVICE\argos-agent sin contrasena
// (CreateService exige NULL, y mgr manda NULL con Password vacio). El SID no
// restringido del servicio hace que la ACL del estado por SID surta efecto.
func serviceConfig() mgr.Config {
	return mgr.Config{
		ServiceType:      windows.SERVICE_WIN32_OWN_PROCESS,
		StartType:        mgr.StartAutomatic,
		DelayedAutoStart: true,
		ErrorControl:     mgr.ErrorNormal,
		ServiceStartName: serviceAccount,
		DisplayName:      DisplayName,
		Description:      Description,
		SidType:          windows.SERVICE_SID_TYPE_UNRESTRICTED,
	}
}

// recoveryActions: reintentos crecientes (5 s, 30 s, 60 s) y luego se rinde hasta que pase el periodo de reinicio del conteo (1 dia).
func recoveryActions() ([]mgr.RecoveryAction, uint32) {
	return []mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 60 * time.Second},
	}, 24 * 60 * 60
}

// Install copia el binario a Program Files, registra el servicio (inicio
// automatico, cuenta virtual, recuperacion ante fallos, fuente de eventos), deja
// el estado en %ProgramData%\argos-agent con la ACL restringida y copia la CA.
// No arranca el servicio: primero hay que enrolarlo.
func (m Manager) Install(_ context.Context, o InstallOptions) error {
	if err := requireElevated(); err != nil {
		return err
	}
	var ca []byte
	if o.CAFile != "" {
		var err error
		if ca, err = LoadCA(o.CAFile); err != nil {
			return err
		}
	}
	src, err := os.Executable()
	if err != nil {
		return fmt.Errorf("ubicando el ejecutable: %w", err)
	}
	dst, err := installExe()
	if err != nil {
		return err
	}
	// Antes de tocar nada: la ruta debe quedar entrecomillada en el servicio.
	if _, err := quotedBinaryPath(dst); err != nil {
		return err
	}

	scm, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("conectando con el administrador de servicios: %w", err)
	}
	defer func() { _ = scm.Disconnect() }() // cierre del handle del SCM; sin accion posible ante un fallo

	if existing, err := scm.OpenService(Name); err == nil {
		_ = existing.Close() // solo se comprobaba que exista
		return ErrAlreadyInstalled
	} else if !errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return fmt.Errorf("consultando el servicio: %w", err)
	}

	if err := copyExecutable(src, dst); err != nil {
		return err
	}
	printf(o.Out, "binario instalado en %s\n", dst)

	s, err := scm.CreateService(Name, dst, serviceConfig())
	if err != nil {
		return fmt.Errorf("creando el servicio: %w", err)
	}
	defer func() { _ = s.Close() }() // cierre del handle del servicio
	if err := m.finishInstall(s, ca, o); err != nil {
		// Sin dejar a medias: si algo posterior falla, se quita el servicio recien creado.
		if delErr := s.Delete(); delErr != nil {
			err = errors.Join(err, fmt.Errorf("y no se pudo quitar el servicio: %w", delErr))
		}
		return err
	}
	return nil
}

func (m Manager) finishInstall(s *mgr.Service, ca []byte, o InstallOptions) error {
	actions, reset := recoveryActions()
	if err := s.SetRecoveryActions(actions, reset); err != nil {
		return fmt.Errorf("configurando la recuperacion ante fallos: %w", err)
	}
	// Tambien reinicia cuando el servicio termina con un codigo de error (no solo si se cae).
	if err := s.SetRecoveryActionsOnNonCrashFailures(true); err != nil {
		return fmt.Errorf("configurando la recuperacion ante errores: %w", err)
	}
	// El servicio no tiene consola: sus logs van al registro de eventos (Application, origen argos-agent).
	if err := eventlog.InstallAsEventCreate(Name, eventlog.Error|eventlog.Warning|eventlog.Info); err != nil &&
		!errors.Is(err, windows.ERROR_ALREADY_EXISTS) && !isRegistryKeyExists(err) {
		return fmt.Errorf("registrando el origen de eventos: %w", err)
	}
	logger := o.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	st := state.NewForService(m.StateDir(), logger, m.StateAccount())
	if err := st.Prepare(); err != nil {
		return err
	}
	if ca != nil {
		if err := st.SaveCA(ca); err != nil {
			return fmt.Errorf("copiando la CA: %w", err)
		}
		printf(o.Out, "CA copiada a %s\n", filepath.Join(m.StateDir(), state.CAFileName))
	}
	printf(o.Out, "servicio %q instalado (cuenta %s, inicio automatico), estado en %s\n", Name, serviceAccount, m.StateDir())
	return nil
}

// isRegistryKeyExists: eventlog.Install devuelve un error de texto si la clave ya existe.
func isRegistryKeyExists(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "already exists")
}

// copyExecutable copia src a dst (crea el directorio; rename atomico).
func copyExecutable(src, dst string) error {
	data, err := os.ReadFile(src) //nolint:gosec // es el propio ejecutable
	if err != nil {
		return fmt.Errorf("leyendo el ejecutable: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil { //nolint:gosec // hereda la ACL de Program Files (solo administradores escriben)
		return fmt.Errorf("creando %s: %w", filepath.Dir(dst), err)
	}
	tmp := dst + ".new"
	if err := os.WriteFile(tmp, data, 0o755); err != nil { //nolint:gosec // ejecutable
		return fmt.Errorf("escribiendo %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp) // best-effort: el error real ya viene en err
		return fmt.Errorf("moviendo el ejecutable a su lugar: %w", err)
	}
	return nil
}

// openService abre el servicio con todos los derechos (administrador).
func openService(scm *mgr.Mgr) (*mgr.Service, error) {
	s, err := scm.OpenService(Name)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return nil, errors.New("el servicio no esta instalado (usa `argos-agent install`)")
		}
		return nil, fmt.Errorf("abriendo el servicio: %w", err)
	}
	return s, nil
}

// Start arranca el servicio.
func (Manager) Start(_ context.Context) error {
	if err := requireElevated(); err != nil {
		return err
	}
	scm, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("conectando con el administrador de servicios: %w", err)
	}
	defer func() { _ = scm.Disconnect() }() // cierre del handle
	s, err := openService(scm)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }() // cierre del handle
	if err := s.Start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
		return fmt.Errorf("arrancando el servicio: %w", err)
	}
	return nil
}

// Stop detiene el servicio y espera a que termine (hasta 30 s).
func (Manager) Stop(ctx context.Context) error {
	if err := requireElevated(); err != nil {
		return err
	}
	scm, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("conectando con el administrador de servicios: %w", err)
	}
	defer func() { _ = scm.Disconnect() }() // cierre del handle
	s, err := openService(scm)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }() // cierre del handle
	return stopAndWait(ctx, s)
}

func stopAndWait(ctx context.Context, s *mgr.Service) error {
	st, err := s.Query()
	if err != nil {
		return fmt.Errorf("consultando el servicio: %w", err)
	}
	if st.State == svc.Stopped {
		return nil
	}
	if st.State != svc.StopPending {
		if _, err := s.Control(svc.Stop); err != nil {
			return fmt.Errorf("deteniendo el servicio: %w", err)
		}
	}
	deadline := time.Now().Add(stopTimeout)
	for {
		if st, err = s.Query(); err != nil {
			return fmt.Errorf("consultando el servicio: %w", err)
		}
		if st.State == svc.Stopped {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("el servicio no se detuvo en %s", stopTimeout)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("esperando la detencion del servicio: %w", ctx.Err())
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// Status resume el estado del servicio; solo pide derecho de consulta, asi que no requiere administrador.
func (Manager) Status(context.Context) (string, error) {
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return "", fmt.Errorf("conectando con el administrador de servicios: %w", err)
	}
	defer func() { _ = windows.CloseServiceHandle(scm) }() // cierre del handle
	name, err := windows.UTF16PtrFromString(Name)
	if err != nil {
		return "", fmt.Errorf("nombre del servicio: %w", err)
	}
	h, err := windows.OpenService(scm, name, windows.SERVICE_QUERY_STATUS)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return "no instalado", nil
	}
	if err != nil {
		return "", fmt.Errorf("abriendo el servicio: %w", err)
	}
	s := &mgr.Service{Name: Name, Handle: h}
	defer func() { _ = s.Close() }() // cierre del handle
	st, err := s.Query()
	if err != nil {
		return "", fmt.Errorf("consultando el servicio: %w", err)
	}
	return "instalado; estado: " + stateName(st.State), nil
}

func stateName(s svc.State) string {
	switch s {
	case svc.Stopped:
		return "detenido"
	case svc.StartPending:
		return "arrancando"
	case svc.StopPending:
		return "deteniendose"
	case svc.Running:
		return "en ejecucion"
	case svc.ContinuePending, svc.PausePending, svc.Paused:
		return "pausado"
	default:
		return fmt.Sprintf("desconocido (%d)", s)
	}
}

// Uninstall detiene y elimina el servicio, su origen de eventos y el binario;
// borra el estado salvo keepState.
func (m Manager) Uninstall(ctx context.Context, keepState bool, out io.Writer) error {
	if err := requireElevated(); err != nil {
		return err
	}
	scm, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("conectando con el administrador de servicios: %w", err)
	}
	defer func() { _ = scm.Disconnect() }() // cierre del handle

	var errs []error
	switch s, err := scm.OpenService(Name); {
	case err == nil:
		errs = append(errs, stopAndWait(ctx, s))
		if delErr := s.Delete(); delErr != nil {
			errs = append(errs, fmt.Errorf("eliminando el servicio: %w", delErr))
		}
		if closeErr := s.Close(); closeErr != nil {
			errs = append(errs, fmt.Errorf("cerrando el servicio: %w", closeErr))
		}
	case errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST):
		printf(out, "el servicio no estaba registrado\n")
	default:
		errs = append(errs, fmt.Errorf("abriendo el servicio: %w", err))
	}
	if err := eventlog.Remove(Name); err != nil {
		printf(out, "aviso: no se pudo quitar el origen de eventos: %v\n", err) // puede no existir
	}
	if exe, err := installExe(); err == nil {
		if err := os.RemoveAll(filepath.Dir(exe)); err != nil {
			// Ocurre si se ejecuto uninstall desde el propio binario instalado: Windows no deja borrar un .exe en uso.
			printf(out, "aviso: no se pudo borrar %s (borralo a mano): %v\n", filepath.Dir(exe), err)
		}
	}
	if !keepState {
		if err := os.RemoveAll(m.StateDir()); err != nil {
			errs = append(errs, fmt.Errorf("borrando %s: %w", m.StateDir(), err))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	printf(out, "servicio desinstalado")
	if keepState {
		printf(out, " (estado conservado en %s)", m.StateDir())
	}
	printf(out, "\n")
	return nil
}
