package agentdist

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeDist crea un directorio de distribucion valido con la version dada.
func writeDist(t *testing.T, version string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, version)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	var entries []string
	for _, tg := range targets {
		name := FileName(tg.os, tg.arch)
		body := []byte("binario " + name)
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		entries = append(entries, fmt.Sprintf(`{"name":%q,"os":%q,"arch":%q,"sha256":%q,"size":%d}`,
			name, tg.os, tg.arch, hex.EncodeToString(sum[:]), len(body)))
	}
	manifest := fmt.Sprintf(`{"version":%q,"files":[%s]}`, version, strings.Join(entries, ","))
	if err := os.WriteFile(filepath.Join(dir, manifestFile), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, latestFile), []byte(version+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestLoadOK(t *testing.T) {
	t.Parallel()
	c, err := Load(writeDist(t, "1.2.3"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Version != "1.2.3" {
		t.Errorf("version got %q, want 1.2.3", c.Version)
	}
	f, meta, ok, err := c.Open("argos-agent_windows_arm64.exe")
	if err != nil || !ok {
		t.Fatalf("Open got ok=%v err=%v", ok, err)
	}
	_ = f.Close()
	if meta.OS != "windows" || meta.Arch != "arm64" {
		t.Errorf("meta got %+v", meta)
	}
	if _, _, ok, _ := c.Open("../latest"); ok {
		t.Error("Open acepto un nombre fuera del mapa")
	}
	// El manifiesto canonico tiene una entrada por linea (lo parsean los scripts con sed).
	lines := strings.Split(string(c.Manifest()), "\n")
	if lines[1] != `"version":"1.2.3",` || !strings.HasPrefix(lines[3], `{"name":"argos-agent_windows_amd64.exe"`) {
		t.Errorf("formato del manifiesto inesperado: %q", lines[:4])
	}
}

func TestLoadRejects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(t *testing.T, root string)
	}{
		{"binario alterado", func(t *testing.T, root string) {
			p := filepath.Join(root, "1.0.0", "argos-agent_linux_amd64")
			if err := os.WriteFile(p, []byte("binario argos-agent_linux_amd64"[:len("binario argos-agent_linux_amd64")-1]+"X"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"tamaño distinto", func(t *testing.T, root string) {
			p := filepath.Join(root, "1.0.0", "argos-agent_linux_arm64")
			if err := os.WriteFile(p, []byte("corto"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"binario ausente", func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, "1.0.0", "argos-agent_darwin_arm64")); err != nil {
				t.Fatal(err)
			}
		}},
		{"manifiesto alterado", func(t *testing.T, root string) {
			p := filepath.Join(root, "1.0.0", manifestFile)
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(strings.Replace(string(b), `"size":`, `"size":9`, 1)), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"manifiesto con campo desconocido", func(t *testing.T, root string) {
			p := filepath.Join(root, "1.0.0", manifestFile)
			if err := os.WriteFile(p, []byte(`{"version":"1.0.0","files":[],"x":1}`), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"version de latest con traversal", func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, latestFile), []byte("../1.0.0\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"latest apunta a otra version que el manifiesto", func(t *testing.T, root string) {
			if err := os.Rename(filepath.Join(root, "1.0.0"), filepath.Join(root, "2.0.0")); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, latestFile), []byte("2.0.0\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"sin latest", func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, latestFile)); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := writeDist(t, "1.0.0")
			tt.mutate(t, root)
			if c, err := Load(root); err == nil {
				t.Fatalf("Load acepto un directorio invalido: %+v", c)
			}
		})
	}
}

func TestOpenDetectsChangeAfterLoad(t *testing.T) {
	t.Parallel()
	root := writeDist(t, "1.0.0")
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "1.0.0", "argos-agent_linux_amd64"), []byte("otro"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := c.Open("argos-agent_linux_amd64"); !ok || err == nil {
		t.Errorf("Open got ok=%v err=%v, want ok y error por tamaño distinto", ok, err)
	}
}
