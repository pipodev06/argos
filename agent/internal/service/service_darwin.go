//go:build darwin

package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"argos/agent/internal/state"
)

const (
	macInstallDir = "/usr/local/libexec/argos-agent"
	macPlistPath  = "/Library/LaunchDaemons/" + launchdLabel + ".plist"
	macStateDir   = "/Library/Application Support/argos-agent"

	dsclPath      = "/usr/bin/dscl"
	launchctlPath = "/bin/launchctl"
	idPath        = "/usr/bin/id"
)

// StateDir es el directorio de estado del servicio.
func (Manager) StateDir() string { return macStateDir }

// StateAccount es el usuario de sistema con el que corre el daemon (dueno del estado).
func (Manager) StateAccount() string { return macUser }

// Installed dice si el plist esta instalado.
func (Manager) Installed() bool {
	_, err := os.Stat(macPlistPath)
	return err == nil
}

// Install crea el usuario _argos (sin login), copia el binario a
// /usr/local/libexec/argos-agent (root), prepara el estado (0700, de _argos),
// copia la CA y escribe el plist en /Library/LaunchDaemons (root:wheel, 0644).
// No carga el daemon: primero hay que enrolarlo (ver Start).
func (m Manager) Install(ctx context.Context, o InstallOptions) error {
	if err := requireRoot(); err != nil {
		return err
	}
	if m.Installed() {
		return ErrAlreadyInstalled
	}
	var ca []byte
	if o.CAFile != "" {
		var err error
		if ca, err = LoadCA(o.CAFile); err != nil {
			return err
		}
	}
	src, err := sourceExecutable()
	if err != nil {
		return err
	}
	if err := ensureMacUser(ctx, o.Out); err != nil {
		return err
	}
	exe, err := installBinary(src, macInstallDir)
	if err != nil {
		return err
	}
	printf(o.Out, "binario instalado en %s\n", exe)

	logger := o.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	st := state.NewForService(macStateDir, logger, macUser)
	if err := st.Prepare(); err != nil {
		return err
	}
	if ca != nil {
		if err := st.SaveCA(ca); err != nil {
			return fmt.Errorf("copiando la CA: %w", err)
		}
		printf(o.Out, "CA copiada a %s/%s\n", macStateDir, state.CAFileName)
	}
	if err := prepareMacLog(); err != nil {
		return err
	}

	plist, err := RenderLaunchdPlist(exe)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(macPlistPath, []byte(plist), 0o644); err != nil { //nolint:gosec // los plist de LaunchDaemons son 0644 root:wheel: no escribibles por group/other
		return err
	}
	if err := os.Chown(macPlistPath, 0, 0); err != nil {
		return fmt.Errorf("propietario del plist: %w", err)
	}
	printf(o.Out, "LaunchDaemon instalado en %s (aun sin cargar)\n", macPlistPath)
	return nil
}

// prepareMacLog crea el archivo de log de _argos (launchd escribe ahi stdout/stderr).
func prepareMacLog() error {
	f, err := os.OpenFile(macLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640) //nolint:gosec // ruta fija
	if err != nil {
		return fmt.Errorf("creando %s: %w", macLogPath, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("cerrando %s: %w", macLogPath, err)
	}
	uid, gid, err := macIDs(context.Background())
	if err != nil {
		return err
	}
	if err := os.Chown(macLogPath, uid, gid); err != nil {
		return fmt.Errorf("propietario de %s: %w", macLogPath, err)
	}
	return nil
}

func macIDs(ctx context.Context) (uid, gid int, err error) {
	for i, flag := range []string{"-u", "-g"} {
		out, err := toolOutput(ctx, idPath, flag, "--", macUser)
		if err != nil {
			return 0, 0, err
		}
		n, err := strconv.Atoi(strings.TrimSpace(out))
		if err != nil {
			return 0, 0, fmt.Errorf("id de %s ilegible: %q", macUser, strings.TrimSpace(out))
		}
		if i == 0 {
			uid = n
		} else {
			gid = n
		}
	}
	return uid, gid, nil
}

// ensureMacUser crea el usuario y el grupo _argos (ocultos, sin shell ni home) si no existen.
func ensureMacUser(ctx context.Context, out io.Writer) error {
	if err := exec.CommandContext(ctx, idPath, "-u", "--", macUser).Run(); err == nil { //nolint:gosec // ruta fija, argumento constante
		return nil
	}
	users, err := toolOutput(ctx, dsclPath, ".", "-list", "/Users", "UniqueID")
	if err != nil {
		return err
	}
	groups, err := toolOutput(ctx, dsclPath, ".", "-list", "/Groups", "PrimaryGroupID")
	if err != nil {
		return err
	}
	id, err := freeMacID(users, groups)
	if err != nil {
		return err
	}
	sid := strconv.Itoa(id)
	steps := [][]string{
		{".", "-create", "/Groups/" + macUser},
		{".", "-create", "/Groups/" + macUser, "PrimaryGroupID", sid},
		{".", "-create", "/Users/" + macUser},
		{".", "-create", "/Users/" + macUser, "UserShell", "/usr/bin/false"},
		{".", "-create", "/Users/" + macUser, "RealName", "Argos agent"},
		{".", "-create", "/Users/" + macUser, "UniqueID", sid},
		{".", "-create", "/Users/" + macUser, "PrimaryGroupID", sid},
		{".", "-create", "/Users/" + macUser, "NFSHomeDirectory", "/var/empty"},
		{".", "-create", "/Users/" + macUser, "IsHidden", "1"},
	}
	for _, args := range steps {
		if err := runTool(ctx, dsclPath, args...); err != nil {
			return fmt.Errorf("creando el usuario %s: %w", macUser, err)
		}
	}
	printf(out, "usuario de sistema %s creado (id %d, sin login)\n", macUser, id)
	return nil
}

func deleteMacUser(ctx context.Context, out io.Writer) error {
	if err := exec.CommandContext(ctx, idPath, "-u", "--", macUser).Run(); err != nil { //nolint:gosec // ruta fija, argumento constante
		return nil // no existe
	}
	var errs []error
	errs = append(errs, runTool(ctx, dsclPath, ".", "-delete", "/Users/"+macUser))
	errs = append(errs, runTool(ctx, dsclPath, ".", "-delete", "/Groups/"+macUser))
	if err := errors.Join(errs...); err != nil {
		return err
	}
	printf(out, "usuario %s eliminado\n", macUser)
	return nil
}

// loaded dice si launchd tiene cargado el daemon.
func macLoaded(ctx context.Context) bool {
	return exec.CommandContext(ctx, launchctlPath, "print", "system/"+launchdLabel).Run() == nil //nolint:gosec // ruta fija
}

// Start carga el daemon (o lo arranca si ya estaba cargado).
func (Manager) Start(ctx context.Context) error {
	if err := requireRoot(); err != nil {
		return err
	}
	if macLoaded(ctx) {
		return runTool(ctx, launchctlPath, "kickstart", "system/"+launchdLabel)
	}
	return runTool(ctx, launchctlPath, "bootstrap", "system", macPlistPath)
}

// Stop descarga el daemon (hasta el proximo arranque del equipo o el proximo Start).
func (Manager) Stop(ctx context.Context) error {
	if err := requireRoot(); err != nil {
		return err
	}
	if !macLoaded(ctx) {
		return nil
	}
	return runTool(ctx, launchctlPath, "bootout", "system/"+launchdLabel)
}

// Status resume el estado del daemon.
func (m Manager) Status(ctx context.Context) (string, error) {
	if !m.Installed() {
		return "no instalado", nil
	}
	out, err := toolOutput(ctx, launchctlPath, "print", "system/"+launchdLabel)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		if l := strings.TrimSpace(line); strings.HasPrefix(l, "state = ") {
			return "instalado; estado: " + strings.TrimPrefix(l, "state = "), nil
		}
	}
	return "instalado; estado: detenido (no cargado; launchctl print puede requerir root)", nil
}

// Uninstall descarga y elimina el daemon y el binario; borra el estado y el
// usuario salvo keepState.
func (m Manager) Uninstall(ctx context.Context, keepState bool, out io.Writer) error {
	if err := requireRoot(); err != nil {
		return err
	}
	var errs []error
	if macLoaded(ctx) {
		errs = append(errs, runTool(ctx, launchctlPath, "bootout", "system/"+launchdLabel))
	}
	errs = append(errs, removeIfExists(macPlistPath), removeIfExists(macInstallDir), removeIfExists(macLogPath))
	if !keepState {
		errs = append(errs, removeIfExists(macStateDir), deleteMacUser(ctx, out))
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	printf(out, "servicio desinstalado")
	if keepState {
		printf(out, " (estado conservado en %s)", macStateDir)
	}
	printf(out, "\n")
	return nil
}
