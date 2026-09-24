//go:build linux || darwin

package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

const toolTimeout = 60 * time.Second

// requireRoot exige root: instalar un servicio escribe en directorios del sistema.
func requireRoot() error {
	if os.Geteuid() != 0 {
		return ErrNotPrivileged
	}
	return nil
}

// runTool ejecuta una herramienta del sistema con timeout; si falla, el error
// incluye su salida (nunca lleva secretos: son comandos de administracion).
func runTool(ctx context.Context, path string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, toolTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, args...).CombinedOutput() //nolint:gosec // ruta fija de una herramienta del sistema
	if err != nil {
		return fmt.Errorf("%s %v: %w: %s", filepath.Base(path), args, err, out)
	}
	return nil
}

// toolOutput ejecuta una herramienta y devuelve su salida aunque termine con codigo
// distinto de cero (systemctl is-active sale con 3 si el servicio esta detenido).
func toolOutput(ctx context.Context, path string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, toolTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, args...).CombinedOutput() //nolint:gosec // ruta fija de una herramienta del sistema
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return "", fmt.Errorf("%s %v: %w", filepath.Base(path), args, err)
	}
	return string(out), nil
}

// sourceExecutable es la ruta real del binario que esta corriendo.
func sourceExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("ubicando el ejecutable: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("resolviendo el ejecutable: %w", err)
	}
	return resolved, nil
}

// verifyRootOnlyDir comprueba que un directorio existente sea de root y no
// escribible por group/other: si no, otro usuario podria reemplazar el binario
// que corre el servicio.
func verifyRootOnlyDir(dir string) error {
	fi, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("revisando %s: %w", dir, err)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("no se pudo leer el propietario de %s", dir)
	}
	if st.Uid != 0 || fi.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%s debe ser de root y no escribible por group/other (uid=%d modo=%o): corrigelo o eliminalo", dir, st.Uid, fi.Mode().Perm())
	}
	return nil
}

// installBinary copia src a dir/argos-agent (dir de root, 0755) de forma atomica y devuelve la ruta destino.
func installBinary(src, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // el binario debe ser ejecutable por la cuenta del servicio; solo root escribe
		return "", fmt.Errorf("creando %s: %w", dir, err)
	}
	if err := verifyRootOnlyDir(dir); err != nil {
		return "", err
	}
	in, err := os.Open(src) //nolint:gosec // es el propio ejecutable
	if err != nil {
		return "", fmt.Errorf("abriendo el ejecutable: %w", err)
	}
	defer func() { _ = in.Close() }() // solo lectura
	data, err := io.ReadAll(in)
	if err != nil {
		return "", fmt.Errorf("leyendo el ejecutable: %w", err)
	}
	dst := filepath.Join(dir, Name)
	if err := writeFileAtomic(dst, data, 0o755); err != nil { //nolint:gosec // ejecutable: 0755 root, no escribible por otros
		return "", err
	}
	return dst, nil
}

// writeFileAtomic escribe path via temporal + fsync + rename, propiedad de root con el modo dado.
func writeFileAtomic(path string, data []byte, mode os.FileMode) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".argos-*.tmp")
	if err != nil {
		return fmt.Errorf("creando temporal en %s: %w", filepath.Dir(path), err)
	}
	name := tmp.Name()
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(name) // best-effort: el error real ya viene en err
		}
	}()
	if err = tmp.Chmod(mode); err != nil {
		return fmt.Errorf("permisos de %s: %w", path, err)
	}
	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("escribiendo %s: %w", path, err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("sincronizando %s: %w", path, err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("cerrando %s: %w", path, err)
	}
	if err = os.Rename(name, path); err != nil {
		return fmt.Errorf("moviendo %s a su lugar: %w", path, err)
	}
	return nil
}

// removeIfExists borra un archivo o arbol; que no exista no es un error.
func removeIfExists(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("borrando %s: %w", path, err)
	}
	return nil
}
