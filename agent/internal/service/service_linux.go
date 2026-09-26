//go:build linux

package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"argos/agent/internal/inventory"
	"argos/agent/internal/state"
)

const (
	linuxInstallDir = "/opt/argos-agent"
	linuxUnitPath   = "/etc/systemd/system/argos-agent.service"
	linuxStateDir   = "/var/lib/argos-agent"
	linuxSudoersDir = "/etc/sudoers.d"
)

var (
	systemctlPaths = []string{"/usr/bin/systemctl", "/bin/systemctl"}
	useraddPaths   = []string{"/usr/sbin/useradd", "/sbin/useradd", "/usr/bin/useradd"}
	userdelPaths   = []string{"/usr/sbin/userdel", "/sbin/userdel", "/usr/bin/userdel"}
	visudoPaths    = []string{"/usr/sbin/visudo", "/usr/bin/visudo", "/sbin/visudo"}
)

// firstExisting devuelve la primera ruta absoluta que exista. Las herramientas del
// sistema se invocan por ruta fija, no por PATH (sudo y los servicios limpian el entorno).
func firstExisting(paths ...string) (string, error) {
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("no se encontro ninguna de estas herramientas: %v", paths)
}

// StateDir es el directorio de estado del servicio (systemd: StateDirectory=argos-agent).
func (Manager) StateDir() string { return linuxStateDir }

// StateAccount es el usuario de sistema con el que corre el servicio (dueno del estado).
func (Manager) StateAccount() string { return Name }

// Installed dice si la unit esta instalada.
func (Manager) Installed() bool {
	_, err := os.Stat(linuxUnitPath)
	return err == nil
}

// Install crea el usuario de sistema argos-agent (sin login ni shell), copia el
// binario a /opt/argos-agent (root), prepara el estado en /var/lib/argos-agent
// (0700, del usuario del servicio), copia la CA y registra la unit endurecida.
// No arranca el servicio: primero hay que enrolarlo.
func (m Manager) Install(ctx context.Context, o InstallOptions) error {
	if err := requireRoot(); err != nil {
		return err
	}
	if m.Installed() {
		return ErrAlreadyInstalled
	}
	systemctl, err := firstExisting(systemctlPaths...)
	if err != nil {
		return fmt.Errorf("systemd es requisito en Linux: %w", err)
	}
	var ca []byte
	if o.CAFile != "" {
		if ca, err = LoadCA(o.CAFile); err != nil {
			return err
		}
	}
	src, err := sourceExecutable()
	if err != nil {
		return err
	}
	if err := ensureLinuxUser(ctx, o.Out); err != nil {
		return err
	}
	exe, err := installBinary(src, linuxInstallDir)
	if err != nil {
		return err
	}
	printf(o.Out, "binario instalado en %s\n", exe)

	logger := o.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	st := state.NewForService(linuxStateDir, logger, Name)
	if err := st.Prepare(); err != nil {
		return err
	}
	if ca != nil {
		if err := st.SaveCA(ca); err != nil {
			return fmt.Errorf("copiando la CA: %w", err)
		}
		printf(o.Out, "CA copiada a %s/%s\n", linuxStateDir, state.CAFileName)
	}

	sudo := false
	if o.Privileged {
		sudo = installSudoers(ctx, o.Out)
		printf(o.Out, "aviso: modo privilegiado: el servicio conserva el usuario %s pero gana las capacidades %s (leer procesos y puertos de otros usuarios). Para volver al modo normal: uninstall e install sin --privileged\n", Name, capsAmbient)
	}
	unit, err := RenderSystemdUnitOpts(exe, UnitOptions{Privileged: o.Privileged, Sudo: sudo})
	if err != nil {
		return err
	}
	if err := writeFileAtomic(linuxUnitPath, []byte(unit), 0o644); err != nil { //nolint:gosec // las units de systemd son legibles por todos; solo root escribe
		return err
	}
	if err := runTool(ctx, systemctl, "daemon-reload"); err != nil {
		return err
	}
	if err := runTool(ctx, systemctl, "enable", Name+".service"); err != nil {
		return err
	}
	printf(o.Out, "unit instalada en %s y habilitada al arranque\n", linuxUnitPath)
	return nil
}

// ensureLinuxUser crea el usuario de sistema si no existe.
func ensureLinuxUser(ctx context.Context, out io.Writer) error {
	if err := exec.CommandContext(ctx, "/usr/bin/id", "-u", "--", Name).Run(); err == nil { //nolint:gosec // ruta fija, argumento constante
		return nil // ya existe
	}
	useradd, err := firstExisting(useraddPaths...)
	if err != nil {
		return fmt.Errorf("no se puede crear el usuario %s: %w", Name, err)
	}
	err = runTool(ctx, useradd, "--system", "--user-group", "--no-create-home",
		"--home-dir", "/nonexistent", "--shell", "/usr/sbin/nologin", "--comment", "argos agent", Name)
	if err != nil {
		return fmt.Errorf("creando el usuario %s: %w", Name, err)
	}
	printf(out, "usuario de sistema %s creado (sin login ni shell)\n", Name)
	return nil
}

// Uninstall detiene y elimina el servicio y el binario; borra el estado y el
// usuario salvo keepState.
func (m Manager) Uninstall(ctx context.Context, keepState bool, out io.Writer) error {
	if err := requireRoot(); err != nil {
		return err
	}
	var errs []error
	if systemctl, err := firstExisting(systemctlPaths...); err == nil {
		if m.Installed() {
			// Parar y deshabilitar pueden fallar si la unit ya estaba parada o deshabilitada: se avisa y se sigue.
			for _, args := range [][]string{{"stop", Name + ".service"}, {"disable", Name + ".service"}} {
				if err := runTool(ctx, systemctl, args...); err != nil {
					printf(out, "aviso: %v\n", err)
				}
			}
		}
		defer func() {
			if err := runTool(ctx, systemctl, "daemon-reload"); err != nil {
				printf(out, "aviso: %v\n", err)
			}
		}()
	}
	errs = append(errs, removeIfExists(linuxUnitPath), removeIfExists(linuxInstallDir),
		removeIfExists(filepath.Join(linuxSudoersDir, SudoersName)))
	if !keepState {
		errs = append(errs, removeIfExists(linuxStateDir))
		errs = append(errs, deleteLinuxUser(ctx, out))
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	printf(out, "servicio desinstalado")
	if keepState {
		printf(out, " (estado conservado en %s)", linuxStateDir)
	}
	printf(out, "\n")
	return nil
}

func deleteLinuxUser(ctx context.Context, out io.Writer) error {
	if err := exec.CommandContext(ctx, "/usr/bin/id", "-u", "--", Name).Run(); err != nil { //nolint:gosec // ruta fija, argumento constante
		return nil // no existe
	}
	userdel, err := firstExisting(userdelPaths...)
	if err != nil {
		return fmt.Errorf("no se puede borrar el usuario %s: %w", Name, err)
	}
	if err := runTool(ctx, userdel, Name); err != nil {
		return err
	}
	printf(out, "usuario %s eliminado\n", Name)
	return nil
}

func (Manager) systemctl(ctx context.Context, args ...string) error {
	if err := requireRoot(); err != nil {
		return err
	}
	systemctl, err := firstExisting(systemctlPaths...)
	if err != nil {
		return err
	}
	return runTool(ctx, systemctl, args...)
}

// Start arranca el servicio.
func (m Manager) Start(ctx context.Context) error { return m.systemctl(ctx, "start", Name+".service") }

// Stop detiene el servicio.
func (m Manager) Stop(ctx context.Context) error { return m.systemctl(ctx, "stop", Name+".service") }

// Status resume el estado del servicio (no requiere root).
func (Manager) Status(ctx context.Context) (string, error) {
	if _, err := os.Stat(linuxUnitPath); errors.Is(err, os.ErrNotExist) {
		return "no instalado", nil
	}
	systemctl, err := firstExisting(systemctlPaths...)
	if err != nil {
		return "", err
	}
	active, err := toolOutput(ctx, systemctl, "is-active", Name+".service")
	if err != nil {
		return "", err
	}
	enabled, err := toolOutput(ctx, systemctl, "is-enabled", Name+".service")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("instalado; estado: %s; al arranque: %s", strings.TrimSpace(active), strings.TrimSpace(enabled)), nil
}

// installSudoers instala /etc/sudoers.d/argos-agent (0440) con los comandos exactos del modulo
// updates, validado con `visudo -cf` antes de dejarlo. Devuelve false (y avisa) si no se pudo:
// el agente sigue con las capacidades pero sin sudo.
func installSudoers(ctx context.Context, out io.Writer) bool {
	exists := func(p string) bool { fi, err := os.Stat(p); return err == nil && !fi.IsDir() }
	if !hasRPMManager(inventory.PrivilegedCommands(), exists) {
		printf(out, "sin gestor RPM (dnf/yum/zypper): no se instala sudoers; apt funciona sin root y el endurecimiento de la unit no se relaja\n")
		return false
	}
	printf(out, "aviso: gestor RPM detectado: se instala sudoers (comandos exactos de solo lectura) y la unit relaja NoNewPrivileges=false y suma CAP_SETUID/CAP_SETGID/CAP_AUDIT_WRITE al bounding set porque sudo es setuid\n")
	visudo, err := firstExisting(visudoPaths...)
	if err != nil {
		printf(out, "aviso: sin visudo no se instala el sudoers; el modulo updates seguira sin sudo (%v)\n", err)
		return false
	}
	content, err := RenderSudoers(Name, inventory.PrivilegedCommands())
	if err != nil {
		printf(out, "aviso: no se genero el sudoers: %v\n", err)
		return false
	}
	validate := func(path string) error { return runTool(ctx, visudo, "-cf", path) }
	if err := installSudoersFile(linuxSudoersDir, SudoersName, []byte(content), validate); err != nil {
		printf(out, "aviso: no se instalo el sudoers; el modulo updates seguira sin sudo: %v\n", err)
		return false
	}
	printf(out, "sudoers instalado en %s (solo comandos exactos de lectura del modulo updates)\n", filepath.Join(linuxSudoersDir, SudoersName))
	return true
}

// ExePath es el binario instalado del servicio.
func (Manager) ExePath() (string, error) { return filepath.Join(linuxInstallDir, Name), nil }
