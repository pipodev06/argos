//go:build linux || darwin

package service

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestLinkCommand(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "bin", Name)
	link := filepath.Join(dir, "usr-bin", Name)

	// No existe: se crea (y el directorio del enlace tambien).
	linkCommand(link, target, io.Discard)
	if got, err := os.Readlink(link); err != nil || got != target {
		t.Fatalf("Readlink = %q, %v; want %q", got, err, target)
	}

	// Symlink previo a otro destino: se reemplaza.
	other := filepath.Join(dir, "otro")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, link); err != nil {
		t.Fatal(err)
	}
	linkCommand(link, target, io.Discard)
	if got, _ := os.Readlink(link); got != target {
		t.Fatalf("Readlink = %q; want %q", got, target)
	}

	// Archivo regular ajeno: no se toca.
	foreign := filepath.Join(dir, "ajeno")
	if err := os.WriteFile(foreign, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkCommand(foreign, target, io.Discard)
	if fi, err := os.Lstat(foreign); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("el archivo ajeno se modifico: %v, %v", fi, err)
	}
}

func TestUnlinkCommand(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, Name)

	ours := filepath.Join(dir, "ours")
	if err := os.Symlink(target, ours); err != nil {
		t.Fatal(err)
	}
	unlinkCommand(ours, target, io.Discard)
	if _, err := os.Lstat(ours); !os.IsNotExist(err) {
		t.Fatalf("el enlace propio sigue: %v", err)
	}

	theirs := filepath.Join(dir, "theirs")
	if err := os.Symlink(filepath.Join(dir, "otro"), theirs); err != nil {
		t.Fatal(err)
	}
	unlinkCommand(theirs, target, io.Discard)
	if _, err := os.Lstat(theirs); err != nil {
		t.Fatalf("se borro un enlace ajeno: %v", err)
	}
}
