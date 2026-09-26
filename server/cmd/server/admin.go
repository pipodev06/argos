package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"argos/server/internal/auth"
	"argos/server/internal/store"
)

const cliTimeout = 30 * time.Second

// runCommand ejecuta un subcomando administrativo (bootstrap y recuperacion).
func runCommand(ctx context.Context, st *store.Store, hasher *auth.Hasher, args []string) error {
	ctx, cancel := context.WithTimeout(ctx, cliTimeout)
	defer cancel()

	switch args[0] {
	case "create-admin":
		return createAdmin(ctx, st, hasher, args[1:])
	case "reset-password":
		return resetPassword(ctx, st, hasher, args[1:])
	default:
		return fmt.Errorf("subcomando %q desconocido (usa create-admin o reset-password)", args[0])
	}
}

// splitMustChange separa el indicador --must-change (en cualquier posicion) del resto de argumentos.
func splitMustChange(args []string) (rest []string, mustChange bool) {
	for _, a := range args {
		if a == "--must-change" {
			mustChange = true
			continue
		}
		rest = append(rest, a)
	}
	return rest, mustChange
}

// createAdmin crea un admin. El username va como argumento o se pide; la contraseña
// se pide sin eco en un terminal, o se lee de stdin si no hay terminal (automatizacion):
// nunca por argumento ni variable de entorno. Con --must-change el usuario debe cambiarla en su primer login (la usa
// el instalador, que genera una contraseña inicial aleatoria y la imprime una sola vez).
func createAdmin(ctx context.Context, st *store.Store, hasher *auth.Hasher, args []string) error {
	args, mustChange := splitMustChange(args)
	in := bufio.NewReader(os.Stdin)
	var username string
	switch len(args) {
	case 0:
		if !term.IsTerminal(int(os.Stdin.Fd())) { // #nosec G115 -- un fd cabe en int
			return errors.New("uso: argos-server create-admin <usuario>")
		}
		fmt.Fprint(os.Stderr, "Usuario: ")
		line, err := in.ReadString('\n')
		if err != nil {
			return fmt.Errorf("leyendo usuario: %w", err)
		}
		username = strings.TrimSpace(line)
	case 1:
		username = strings.TrimSpace(args[0])
	default:
		return errors.New("uso: argos-server create-admin [usuario]")
	}
	if err := auth.ValidateUsername(username); err != nil {
		return err
	}

	password, err := readNewPassword(in)
	if err != nil {
		return err
	}
	hash, err := hasher.Hash(ctx, password)
	if err != nil {
		return err
	}
	u, err := st.CreateUser(ctx, store.NewUser{Username: username, PasswordHash: hash, Role: store.RoleAdmin, MustChange: mustChange, AllGroups: true})
	if err != nil {
		return fmt.Errorf("creando admin: %w", err)
	}
	auditCLI(ctx, st, u, "user_created")
	fmt.Fprintf(os.Stderr, "admin %q creado\n", u.Username)
	return nil
}

// resetPassword fija una contraseña nueva a <usuario>, lo desbloquea y cierra sus
// sesiones. No depende del login ni de un admin: es la via de recuperacion.
func resetPassword(ctx context.Context, st *store.Store, hasher *auth.Hasher, args []string) error {
	if len(args) != 1 {
		return errors.New("uso: argos-server reset-password <usuario>")
	}
	password, err := readNewPassword(bufio.NewReader(os.Stdin))
	if err != nil {
		return err
	}
	hash, err := hasher.Hash(ctx, password)
	if err != nil {
		return err
	}
	u, err := st.ResetPasswordByUsername(ctx, strings.TrimSpace(args[0]), hash, time.Now())
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("el usuario %q no existe", args[0])
	}
	if err != nil {
		return fmt.Errorf("reseteando contraseña: %w", err)
	}
	auditCLI(ctx, st, u, "user_password_reset")
	fmt.Fprintf(os.Stderr, "contraseña de %q actualizada, cuenta desbloqueada y sesiones cerradas\n", u.Username)
	return nil
}

// auditCLI deja rastro de la accion en audit_log. Sin actor: lo hace quien tiene acceso al servidor.
func auditCLI(ctx context.Context, st *store.Store, target store.User, event string) {
	err := st.InsertAudit(ctx, store.AuditEntry{
		Event: event, Outcome: store.OutcomeOK, TargetType: "user", TargetID: target.ID,
		Detail: []byte(`{"via":"cli"}`),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "aviso: no se pudo escribir la auditoria: %v\n", err)
	}
}

// readNewPassword lee la contraseña y valida la politica. En un terminal la pide dos
// veces sin eco; sin terminal toma la primera linea de stdin.
func readNewPassword(in *bufio.Reader) (string, error) {
	fd := int(os.Stdin.Fd()) // #nosec G115 -- un fd cabe en int
	if !term.IsTerminal(fd) {
		line, err := in.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("leyendo contraseña de stdin: %w", err)
		}
		pw := strings.TrimRight(line, "\r\n")
		if err := auth.CheckPassword(pw); err != nil {
			return "", err
		}
		return pw, nil
	}

	fmt.Fprint(os.Stderr, "Contraseña nueva: ")
	first, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("leyendo contraseña: %w", err)
	}
	fmt.Fprint(os.Stderr, "Repite la contraseña: ")
	second, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("leyendo contraseña: %w", err)
	}
	if string(first) != string(second) {
		return "", errors.New("las contraseñas no coinciden")
	}
	if err := auth.CheckPassword(string(first)); err != nil {
		return "", err
	}
	return string(first), nil
}
