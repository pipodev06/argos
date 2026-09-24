package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"argos/agent/internal/state"
)

func stateDir() (string, error) { return filepath.Join("base", "cfg", "argos-agent"), nil }

func TestLoad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		env          map[string]string
		wantInterval time.Duration
		wantDisk     time.Duration
		wantInsecure bool
		wantErr      bool
	}{
		{name: "vacio es valido (cada subcomando exige lo suyo)", env: map[string]string{}, wantInterval: 30 * time.Second},
		{name: "intervalo explicito", env: map[string]string{"ARGOS_SERVER_URL": "https://argos.example.com", "ARGOS_INTERVAL": "1m"}, wantInterval: time.Minute},
		{name: "url sin esquema", env: map[string]string{"ARGOS_SERVER_URL": "localhost:8080"}, wantErr: true},
		{name: "esquema no http", env: map[string]string{"ARGOS_SERVER_URL": "ftp://x"}, wantErr: true},
		{name: "credenciales en la url", env: map[string]string{"ARGOS_SERVER_URL": "https://u:p@x"}, wantErr: true},
		{name: "intervalo invalido", env: map[string]string{"ARGOS_INTERVAL": "rapido"}, wantErr: true},
		{name: "intervalo demasiado corto", env: map[string]string{"ARGOS_INTERVAL": "1s"}, wantErr: true},
		{name: "disco 2m", env: map[string]string{"ARGOS_DISK_INTERVAL": "2m"}, wantInterval: 30 * time.Second, wantDisk: 2 * time.Minute},
		{name: "disco minimo exacto", env: map[string]string{"ARGOS_DISK_INTERVAL": "60s"}, wantInterval: 30 * time.Second, wantDisk: time.Minute},
		{name: "disco demasiado corto", env: map[string]string{"ARGOS_DISK_INTERVAL": "59s"}, wantErr: true},
		{name: "disco invalido", env: map[string]string{"ARGOS_DISK_INTERVAL": "seguido"}, wantErr: true},
		{name: "insecure 1", env: map[string]string{"ARGOS_ALLOW_INSECURE": "1"}, wantInterval: 30 * time.Second, wantInsecure: true},
		{name: "insecure 0", env: map[string]string{"ARGOS_ALLOW_INSECURE": "0"}, wantInterval: 30 * time.Second},
		{name: "insecure basura", env: map[string]string{"ARGOS_ALLOW_INSECURE": "quizas"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := Load(func(k string) string { return tt.env[k] }, stateDir)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if cfg.Interval != tt.wantInterval {
				t.Errorf("Interval got %v, want %v", cfg.Interval, tt.wantInterval)
			}
			if cfg.AllowInsecure != tt.wantInsecure {
				t.Errorf("AllowInsecure got %v, want %v", cfg.AllowInsecure, tt.wantInsecure)
			}
		})
	}
}

func TestLoadStateDir(t *testing.T) {
	t.Parallel()

	got, err := Load(func(string) string { return "" }, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("base", "cfg", "argos-agent"); got.StateDir != want {
		t.Errorf("StateDir por defecto got %q, want %q", got.StateDir, want)
	}

	got, err = Load(func(k string) string {
		if k == "ARGOS_STATE_DIR" {
			return "/var/lib/argos"
		}
		return ""
	}, func() (string, error) { return "", errors.New("no debe llamarse") })
	if err != nil || got.StateDir != "/var/lib/argos" {
		t.Errorf("ARGOS_STATE_DIR got %q err %v", got.StateDir, err)
	}

	if _, err := Load(func(string) string { return "" }, func() (string, error) { return "", errors.New("sin HOME") }); err == nil {
		t.Error("sin ARGOS_STATE_DIR ni directorio de usuario se esperaba error")
	}
}

func TestValidateServerURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		url           string
		allowInsecure bool
		wantWarn      bool
		wantErr       error
		wantAnyErr    bool
	}{
		{url: "http://localhost:8080"},
		{url: "http://127.0.0.1:8080"},
		{url: "http://[::1]:8080"},
		{url: "https://argos.example.com"},
		{url: "http://argos.example.com", wantErr: ErrInsecureTransport},
		{url: "http://10.0.0.5:8080", wantErr: ErrInsecureTransport},
		{url: "http://10.0.0.5:8080", allowInsecure: true, wantWarn: true},
		{url: "https://argos.example.com", allowInsecure: true},
		{url: "http://localhost:8080", allowInsecure: true},
		{url: "gopher://x", wantAnyErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			t.Parallel()
			warn, err := ValidateServerURL(tt.url, tt.allowInsecure)
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("err got %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && (err != nil) != tt.wantAnyErr {
				t.Fatalf("err got %v, wantAnyErr %v", err, tt.wantAnyErr)
			}
			if warn != tt.wantWarn {
				t.Errorf("insecureRemote got %v, want %v", warn, tt.wantWarn)
			}
		})
	}
}

func TestLoadUsesCAFromStateDirByDefault(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	env := map[string]string{"ARGOS_STATE_DIR": dir}
	get := func(k string) string { return env[k] }
	noDir := func() (string, error) { return "", errors.New("no debe llamarse") }

	cfg, err := Load(get, noDir)
	if err != nil || cfg.CAFile != "" {
		t.Fatalf("sin ca.pem: CAFile got %q err %v, want vacio", cfg.CAFile, err)
	}
	ca := filepath.Join(dir, state.CAFileName)
	if err := os.WriteFile(ca, []byte("pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	if cfg, err = Load(get, noDir); err != nil || cfg.CAFile != ca {
		t.Errorf("con ca.pem: CAFile got %q err %v, want %q", cfg.CAFile, err, ca)
	}
	env["ARGOS_CA_FILE"] = "/otra/ca.pem" // la variable explicita gana
	if cfg, err = Load(get, noDir); err != nil || cfg.CAFile != "/otra/ca.pem" {
		t.Errorf("con ARGOS_CA_FILE: CAFile got %q err %v", cfg.CAFile, err)
	}
}
