//go:build !windows

package state

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const idPath = "/usr/bin/id" // ruta absoluta: sin PATH; presente en Linux y macOS

func restrictDir(dir, serviceAccount string) error {
	// MkdirAll respeta el umask y no toca un directorio que ya existia.
	if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // 0700 es el minimo pa que el dueno entre al directorio
		return fmt.Errorf("chmod 0700: %w", err)
	}
	return chownTo(dir, serviceAccount)
}

func restrictFile(path, serviceAccount string) error {
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod 0600: %w", err)
	}
	return chownTo(path, serviceAccount)
}

// chownTo deja path a nombre de la cuenta del servicio (solo si hay una): el
// servicio corre sin privilegios y no podria leer un archivo 0600 de root.
func chownTo(path, account string) error {
	if account == "" {
		return nil
	}
	uid, gid, err := lookupIDs(account)
	if err != nil {
		return err
	}
	if err := os.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("chown a %s: %w", account, err)
	}
	return nil
}

// lookupIDs resuelve uid y gid con id(1): os/user sin cgo (CGO_ENABLED=0) no ve
// los usuarios de Directory Services en macOS.
func lookupIDs(account string) (uid, gid int, err error) {
	if uid, err = idOf("-u", account); err != nil {
		return 0, 0, err
	}
	if gid, err = idOf("-g", account); err != nil {
		return 0, 0, err
	}
	return uid, gid, nil
}

func idOf(flag, account string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, idPath, flag, "--", account).Output() //nolint:gosec // ruta fija; account es una constante del programa
	if err != nil {
		return 0, fmt.Errorf("el usuario %q no existe (id %s): %w", account, flag, err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || n < 0 {
		return 0, fmt.Errorf("id de %q ilegible: %q", account, strings.TrimSpace(string(out)))
	}
	return n, nil
}
