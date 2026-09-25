package state

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// En Windows, renombrar sobre un archivo abierto (sin FILE_SHARE_DELETE) da "Acceso denegado" hasta que se cierra.
func TestRenameWithRetryWaitsForAHeldTarget(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("el bloqueo de archivos abiertos es propio de Windows")
	}
	dir := t.TempDir()
	from, to := filepath.Join(dir, "new.tmp"), filepath.Join(dir, "agent.json")
	if err := os.WriteFile(from, []byte("nuevo"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, []byte("viejo"), 0o600); err != nil {
		t.Fatal(err)
	}
	held, err := os.Open(to)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(120 * time.Millisecond)
		_ = held.Close()
	}()
	if err := renameWithRetry(from, to); err != nil {
		t.Fatalf("renameWithRetry no esperó a que se liberara el archivo: %v", err)
	}
	if got, _ := os.ReadFile(to); string(got) != "nuevo" {
		t.Errorf("contenido %q, quería \"nuevo\"", got)
	}
}

func TestRenameWithRetryReturnsOtherErrorsImmediately(t *testing.T) {
	dir := t.TempDir()
	start := time.Now()
	if err := renameWithRetry(filepath.Join(dir, "no-existe.tmp"), filepath.Join(dir, "x")); err == nil {
		t.Fatal("quería un error por origen inexistente")
	}
	if time.Since(start) > 150*time.Millisecond {
		t.Errorf("un error que no es de permiso no debe reintentarse (tardó %v)", time.Since(start))
	}
}
