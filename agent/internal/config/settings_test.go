package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"argos/agent/internal/inventory"
)

type memWriter struct{ dir string }

func (m memWriter) WriteFile(name string, data []byte) error {
	return os.WriteFile(filepath.Join(m.dir, name), data, 0o600)
}

func TestSettingsNormalize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      Settings
		want    Settings
		wantErr string
	}{
		{"vacio", Settings{}, Settings{}, ""},
		{"all expande", Settings{Inventory: "all"}, Settings{Inventory: "ports,services,processes,updates"}, ""},
		{"sin repetidos y minusculas", Settings{Inventory: "Ports, ports,updates"}, Settings{Inventory: "ports,updates"}, ""},
		{"modulo desconocido", Settings{Inventory: "ports,root"}, Settings{}, "--inventory"},
		{"cidr valido", Settings{ProbeAllow: " 192.168.1.0/25 , 10.0.0.5"}, Settings{ProbeAllow: "192.168.1.0/25 , 10.0.0.5"}, ""},
		{"todo IPv4", Settings{ProbeAllow: "0.0.0.0/0"}, Settings{}, "--probe-allow"},
		{"todo IPv6", Settings{ProbeAllow: "::/0"}, Settings{}, "--probe-allow"},
		{"muy grande", Settings{ProbeAllow: "10.0.0.0/8"}, Settings{}, "--probe-allow"},
		{"loopback", Settings{ProbeAllow: "127.0.0.0/24"}, Settings{}, "--probe-allow"},
		{"basura", Settings{ProbeAllow: "192.168.1.0/24;rm -rf /"}, Settings{}, "--probe-allow"},
		{"insecure pasa", Settings{Insecure: true}, Settings{Insecure: true}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.in.Normalize()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err got %v, want que contenga %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Normalize: %v", err)
			}
			if got.Inventory != tt.want.Inventory || got.Insecure != tt.want.Insecure || strings.TrimSpace(got.ProbeAllow) != strings.TrimSpace(tt.want.ProbeAllow) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLoadUsesSettingsWithEnvPrecedence(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := SaveSettings(memWriter{dir}, Settings{Inventory: "ports,services", ProbeAllow: "192.168.5.0/24", Insecure: true}); err != nil {
		t.Fatal(err)
	}
	load := func(env map[string]string) Config {
		t.Helper()
		env["ARGOS_STATE_DIR"] = dir
		cfg, err := Load(func(k string) string { return env[k] }, func() (string, error) { return dir, nil })
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		return cfg
	}

	t.Run("solo archivo", func(t *testing.T) {
		cfg := load(map[string]string{})
		if !slices.Equal(cfg.InventoryModules, []string{inventory.ModulePorts, inventory.ModuleServices}) {
			t.Errorf("InventoryModules got %v", cfg.InventoryModules)
		}
		if !cfg.ProbeAllow.Enabled() || !cfg.AllowInsecure {
			t.Errorf("ProbeAllow=%v AllowInsecure=%v, want desde el archivo", cfg.ProbeAllow, cfg.AllowInsecure)
		}
	})
	t.Run("el entorno gana", func(t *testing.T) {
		cfg := load(map[string]string{"ARGOS_INVENTORY_MODULES": "processes", "ARGOS_ALLOW_INSECURE": "0"})
		if !slices.Equal(cfg.InventoryModules, []string{inventory.ModuleProcesses}) {
			t.Errorf("InventoryModules got %v, want [processes]", cfg.InventoryModules)
		}
		if cfg.AllowInsecure {
			t.Error("ARGOS_ALLOW_INSECURE=0 debe pisar insecure=true del archivo")
		}
	})
}

func TestLoadWithoutSettingsKeepsDefaults(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg, err := Load(func(string) string { return "" }, func() (string, error) { return dir, nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.InventoryModules) != 0 || cfg.AllowInsecure || cfg.ProbeAllow.Enabled() {
		t.Errorf("sin archivo ni entorno todo debe estar apagado: %+v", cfg)
	}
}

func TestLoadSettingsRejectsGarbage(t *testing.T) {
	t.Parallel()
	for name, content := range map[string]string{
		"no es json":      "hola",
		"campo extrano":   `{"inventory":"ports","x":1}`,
		"tipo equivocado": `{"insecure":"si"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, SettingsFileName), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadSettings(dir); err == nil {
				t.Error("un settings.json ilegible debe fallar con error claro")
			}
		})
	}
}
