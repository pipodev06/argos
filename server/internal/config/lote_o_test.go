package config

import (
	"log/slog"
	"strings"
	"testing"
)

func TestLoadLogLevel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		raw     string
		want    slog.Level
		wantErr bool
	}{
		{raw: "", want: slog.LevelInfo},
		{raw: "debug", want: slog.LevelDebug},
		{raw: "INFO", want: slog.LevelInfo},
		{raw: "warn", want: slog.LevelWarn},
		{raw: "error", want: slog.LevelError},
		{raw: "verbose", wantErr: true},
		{raw: "3", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			t.Parallel()
			env := map[string]string{"DATABASE_URL": "postgres://x", "ARGOS_LOG_LEVEL": tt.raw}
			cfg, err := Load(func(k string) string { return env[k] })
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "ARGOS_LOG_LEVEL") {
					t.Fatalf("err got %v, want un error que nombre ARGOS_LOG_LEVEL", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.LogLevel != tt.want {
				t.Errorf("LogLevel got %v, want %v", cfg.LogLevel, tt.want)
			}
		})
	}
}

func TestProxyWarning(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		env  map[string]string
		warn bool
	}{
		{name: "solo loopback sin url publica", env: map[string]string{}, warn: false},
		{name: "loopback con url http", env: map[string]string{"ARGOS_PUBLIC_URL": "http://localhost:8080"}, warn: false},
		{name: "url https sin proxies", env: map[string]string{"ARGOS_PUBLIC_URL": "https://argos.example.com"}, warn: true},
		{name: "url https con proxies", env: map[string]string{"ARGOS_PUBLIC_URL": "https://argos.example.com", "ARGOS_TRUSTED_PROXIES": "127.0.0.1"}, warn: false},
		{name: "escucha en todas las interfaces", env: map[string]string{"ARGOS_LISTEN_ADDR": "0.0.0.0:8080"}, warn: true},
		{name: "escucha en una IP de LAN", env: map[string]string{"ARGOS_LISTEN_ADDR": "192.168.1.5:8080"}, warn: true},
		{name: "escucha en todas con proxies", env: map[string]string{"ARGOS_LISTEN_ADDR": ":8080", "ARGOS_TRUSTED_PROXIES": "10.0.0.0/8"}, warn: false},
		{name: "localhost", env: map[string]string{"ARGOS_LISTEN_ADDR": "localhost:8080"}, warn: false},
		{name: "ipv6 loopback", env: map[string]string{"ARGOS_LISTEN_ADDR": "[::1]:8080"}, warn: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.env["DATABASE_URL"] = "postgres://x"
			cfg, err := Load(func(k string) string { return tt.env[k] })
			if err != nil {
				t.Fatal(err)
			}
			msg := cfg.ProxyWarning()
			if (msg != "") != tt.warn {
				t.Errorf("ProxyWarning got %q, want warn=%v", msg, tt.warn)
			}
			if tt.warn && !strings.Contains(msg, "ARGOS_TRUSTED_PROXIES") {
				t.Errorf("el aviso no nombra la variable: %q", msg)
			}
		})
	}
}
