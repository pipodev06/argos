package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SudoersName es el archivo del drop-in en /etc/sudoers.d (root:root 0440).
const SudoersName = "argos-agent"

// sudoersToken acepta solo caracteres inocuos: nada de comodines, comas, dos puntos, iguales,
// barras invertidas ni espacios dentro de un argumento (el sudoers los interpreta).
var sudoersToken = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// RenderSudoers genera el drop-in que deja a user ejecutar como root SOLO los comandos dados
// (ruta absoluta + argumentos fijos), sin contrasena. Una linea por comando. Rechaza rutas
// relativas y cualquier token con caracteres especiales del sudoers (sin comodines ni ALL).
func RenderSudoers(user string, cmds [][]string) (string, error) {
	if !sudoersToken.MatchString(user) {
		return "", fmt.Errorf("usuario invalido pa el sudoers: %q", user)
	}
	if len(cmds) == 0 {
		return "", errors.New("el sudoers no tiene comandos")
	}
	var b strings.Builder
	b.WriteString("# Generado por `argos-agent install --privileged`. Comandos EXACTOS de solo lectura del modulo updates.\n")
	for _, c := range cmds {
		if len(c) == 0 || !strings.HasPrefix(c[0], "/") {
			return "", fmt.Errorf("comando sin ruta absoluta: %v", c)
		}
		for _, tok := range c {
			if !sudoersToken.MatchString(tok) || tok == "ALL" {
				return "", fmt.Errorf("token no permitido en el sudoers: %q", tok)
			}
		}
		fmt.Fprintf(&b, "%s ALL=(root) NOPASSWD: %s\n", user, strings.Join(c, " "))
	}
	return b.String(), nil
}

// sudoersMode: sudo exige que el drop-in no sea escribible por group/other; 0440 root:root.
const sudoersMode = 0o440

// installSudoersFile escribe data en dir/name (modo 0440) SOLO si validate(temporal) lo aprueba.
// El temporal lleva un punto en el nombre, asi que sudo lo ignora mientras se valida. Quien
// llama corre como root, por eso el archivo queda root:root. Si validate falla no queda nada.
func installSudoersFile(dir, name string, data []byte, validate func(path string) error) (err error) {
	tmp, err := os.CreateTemp(dir, ".argos-*.tmp")
	if err != nil {
		return fmt.Errorf("creando temporal en %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName) // best-effort: el error real ya viene en err
		}
	}()
	if err = tmp.Chmod(sudoersMode); err != nil {
		return fmt.Errorf("permisos del sudoers: %w", err)
	}
	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("escribiendo el sudoers: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("cerrando el sudoers: %w", err)
	}
	if err = validate(tmpName); err != nil {
		return fmt.Errorf("sudoers rechazado: %w", err)
	}
	if err = os.Rename(tmpName, filepath.Join(dir, name)); err != nil {
		return fmt.Errorf("moviendo el sudoers a su lugar: %w", err)
	}
	return nil
}

// hasRPMManager dice si hay dnf, yum o zypper (rutas absolutas de la lista de comandos del modulo
// updates). exists es inyectable pa probarlo sin tocar el SO.
func hasRPMManager(cmds [][]string, exists func(path string) bool) bool {
	for _, c := range cmds {
		if exists(c[0]) {
			return true
		}
	}
	return false
}
