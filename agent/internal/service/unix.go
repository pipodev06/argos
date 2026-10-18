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

const (
	toolTimeout = 60 * time.Second

	// binLink es el enlace que deja `argos-agent` en el PATH de cualquier usuario.
	binLink = "/usr/local/bin/" + Name
)

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

// RequirePrivileges exige root (actualizar el binario del servicio escribe en directorios del sistema).
func (Manager) RequirePrivileges() error { return requireRoot() }

// linkCommand crea (o reemplaza) el symlink link -> target pa que `argos-agent` funcione desde
// cualquier carpeta. Solo toca algo que no exista o que ya sea un symlink; un archivo ajeno se
// respeta. Un fallo solo avisa: el servicio funciona igual.
func linkCommand(link, target string, out io.Writer) {
	if fi, err := os.Lstat(link); err == nil {
		if fi.Mode()&os.ModeSymlink == 0 {
			printf(out, "aviso: %s ya existe y no es un enlace; no se toca (usa %s)\n", link, target)
			return
		}
		if cur, err := os.Readlink(link); err == nil && cur == target {
			return
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		printf(out, "aviso: no se pudo revisar %s: %v\n", link, err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil { //nolint:gosec // directorio estandar del sistema
		printf(out, "aviso: no se pudo crear %s: %v\n", filepath.Dir(link), err)
		return
	}
	// Enlace temporal + rename: reemplazo atomico sin ventana en que falte el comando.
	tmp := link + ".new"
	_ = os.Remove(tmp) // resto de un intento previo; si no existe no pasa nada
	if err := os.Symlink(target, tmp); err != nil {
		printf(out, "aviso: no se pudo crear el enlace %s: %v\n", link, err)
		return
	}
	if err := os.Rename(tmp, link); err != nil {
		_ = os.Remove(tmp) // best-effort: el error real ya se avisa
		printf(out, "aviso: no se pudo crear el enlace %s: %v\n", link, err)
		return
	}
	printf(out, "comando %s disponible en el PATH (%s -> %s)\n", Name, link, target)
}

// unlinkCommand borra link solo si es un symlink que apunta a target.
func unlinkCommand(link, target string, out io.Writer) {
	cur, err := os.Readlink(link)
	if err != nil || cur != target {
		return // no existe, no es symlink o apunta a otra cosa: no es nuestro
	}
	if err := os.Remove(link); err != nil {
		printf(out, "aviso: no se pudo borrar %s: %v\n", link, err)
	}
}
