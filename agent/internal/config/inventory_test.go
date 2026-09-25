package config

import (
	"reflect"
	"testing"
	"time"
)

func TestLoadInventoryModules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		raw     string
		want    []string
		wantErr bool
	}{
		{raw: "", want: nil}, // apagado por defecto
		{raw: "ports, services", want: []string{"ports", "services"}},
		{raw: "ports,gpu", wantErr: true},
	}
	for _, tt := range tests {
		cfg, err := Load(func(k string) string {
			if k == "ARGOS_INVENTORY_MODULES" {
				return tt.raw
			}
			return ""
		}, stateDir)
		if (err != nil) != tt.wantErr || !reflect.DeepEqual(cfg.InventoryModules, tt.want) {
			t.Errorf("modules %q = %v, %v; want %v (err=%v)", tt.raw, cfg.InventoryModules, err, tt.want, tt.wantErr)
		}
	}
}

func TestLoadUpdatesConfig(t *testing.T) {
	t.Parallel()
	stateDir := func() (string, error) { return t.TempDir(), nil }
	tests := []struct {
		name         string
		interval     string
		online       string
		wantInterval time.Duration
		wantOnline   string
		wantErr      bool
	}{
		{"defectos", "", "", 6 * time.Hour, "auto", false},
		{"valores", "12h", "never", 12 * time.Hour, "never", false},
		{"siempre en linea", "1h", "ALWAYS", time.Hour, "always", false},
		{"intervalo bajo el minimo", "30m", "", 0, "", true},
		{"intervalo sobre el maximo", "48h", "", 0, "", true},
		{"intervalo basura", "mucho", "", 0, "", true},
		{"modo invalido", "", "si", 0, "", true},
	}
	for _, tt := range tests {
		cfg, err := Load(func(k string) string {
			switch k {
			case "ARGOS_UPDATES_INTERVAL":
				return tt.interval
			case "ARGOS_UPDATES_WINDOWS_ONLINE":
				return tt.online
			}
			return ""
		}, stateDir)
		if (err != nil) != tt.wantErr {
			t.Errorf("%s: err=%v, wantErr=%t", tt.name, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && (cfg.Updates.Interval != tt.wantInterval || cfg.Updates.WindowsOnline != tt.wantOnline) {
			t.Errorf("%s: got %v/%q, want %v/%q", tt.name, cfg.Updates.Interval, cfg.Updates.WindowsOnline, tt.wantInterval, tt.wantOnline)
		}
	}
}

func TestLoadUpdatesSudo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		raw     string
		want    bool
		wantErr bool
	}{
		{raw: "", want: false},
		{raw: "true", want: true},
		{raw: "1", want: true},
		{raw: "no", wantErr: true},
	}
	for _, tt := range tests {
		cfg, err := Load(func(k string) string {
			if k == "ARGOS_UPDATES_SUDO" {
				return tt.raw
			}
			return ""
		}, stateDir)
		if (err != nil) != tt.wantErr || cfg.Updates.UseSudo != tt.want {
			t.Errorf("sudo %q = %v, %v; want %v (err=%v)", tt.raw, cfg.Updates.UseSudo, err, tt.want, tt.wantErr)
		}
	}
}
