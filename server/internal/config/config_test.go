package config

import (
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	t.Parallel()

	const db = "postgres://x"
	tests := []struct {
		name     string
		env      map[string]string
		wantAddr string
		wantErr  bool
	}{
		{name: "default escucha solo en loopback", env: map[string]string{"DATABASE_URL": db}, wantAddr: "127.0.0.1:8080"},
		{name: "addr explicita", env: map[string]string{"DATABASE_URL": db, "ARGOS_LISTEN_ADDR": "0.0.0.0:9000"}, wantAddr: "0.0.0.0:9000"},
		{name: "sin database url", env: map[string]string{}, wantErr: true},
		{name: "addr sin puerto", env: map[string]string{"DATABASE_URL": db, "ARGOS_LISTEN_ADDR": "localhost"}, wantErr: true},
		{name: "puerto fuera de rango", env: map[string]string{"DATABASE_URL": db, "ARGOS_LISTEN_ADDR": ":70000"}, wantErr: true},
		{name: "puerto no numerico", env: map[string]string{"DATABASE_URL": db, "ARGOS_LISTEN_ADDR": ":http"}, wantErr: true},
		{name: "public url valida", env: map[string]string{"DATABASE_URL": db, "ARGOS_PUBLIC_URL": "https://argos.example.com"}, wantAddr: "127.0.0.1:8080"},
		{name: "public url con ruta", env: map[string]string{"DATABASE_URL": db, "ARGOS_PUBLIC_URL": "https://argos.example.com/app"}, wantErr: true},
		{name: "public url sin esquema", env: map[string]string{"DATABASE_URL": db, "ARGOS_PUBLIC_URL": "argos.example.com"}, wantErr: true},
		{name: "proxies validos", env: map[string]string{"DATABASE_URL": db, "ARGOS_TRUSTED_PROXIES": "10.0.0.0/8, 192.168.1.5"}, wantAddr: "127.0.0.1:8080"},
		{name: "proxy invalido", env: map[string]string{"DATABASE_URL": db, "ARGOS_TRUSTED_PROXIES": "no-es-ip"}, wantErr: true},
		{name: "tls solo cert", env: map[string]string{"DATABASE_URL": db, "ARGOS_TLS_CERT": "c.pem"}, wantErr: true},
		{name: "tls completo", env: map[string]string{"DATABASE_URL": db, "ARGOS_TLS_CERT": "c.pem", "ARGOS_TLS_KEY": "k.pem"}, wantAddr: "127.0.0.1:8080"},
		{name: "retencion de audit bajo el minimo", env: map[string]string{"DATABASE_URL": db, "ARGOS_AUDIT_RETENTION_DAYS": "29"}, wantErr: true},
		{name: "retencion de audit valida", env: map[string]string{"DATABASE_URL": db, "ARGOS_AUDIT_RETENTION_DAYS": "30"}, wantAddr: "127.0.0.1:8080"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := Load(func(k string) string { return tt.env[k] })
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && cfg.ListenAddr != tt.wantAddr {
				t.Errorf("ListenAddr got %q, want %q", cfg.ListenAddr, tt.wantAddr)
			}
		})
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := Load(func(k string) string {
		return map[string]string{"DATABASE_URL": "postgres://x", "ARGOS_TRUSTED_PROXIES": "10.0.0.7"}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HostDownAfter != 2*time.Minute || cfg.HostCheckInterval != 15*time.Second {
		t.Errorf("defaults del detector got %s/%s, want 2m/15s", cfg.HostDownAfter, cfg.HostCheckInterval)
	}
	if cfg.AuditRetentionDays != 180 || cfg.PublicURL != nil || cfg.InsecureCookies {
		t.Errorf("defaults inesperados: %+v", cfg)
	}
	if len(cfg.TrustedProxies) != 1 || cfg.TrustedProxies[0].String() != "10.0.0.7/32" {
		t.Errorf("TrustedProxies got %v, want [10.0.0.7/32]", cfg.TrustedProxies)
	}
}

func TestLoadHostDetector(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		downAfter    string
		interval     string
		wantDownAt   time.Duration
		wantInterval time.Duration
		wantErr      bool
	}{
		{name: "defaults", wantDownAt: 2 * time.Minute, wantInterval: 15 * time.Second},
		{name: "valores validos", downAfter: "5m", interval: "30s", wantDownAt: 5 * time.Minute, wantInterval: 30 * time.Second},
		{name: "minimo permitido", downAfter: "30s", interval: "1s", wantDownAt: 30 * time.Second, wantInterval: time.Second},
		{name: "down after bajo el minimo", downAfter: "29s", wantErr: true},
		{name: "down after cero", downAfter: "0s", wantErr: true},
		{name: "down after negativo", downAfter: "-2m", wantErr: true},
		{name: "down after sin unidad", downAfter: "120", wantErr: true},
		{name: "down after basura", downAfter: "pronto", wantErr: true},
		{name: "interval cero", interval: "0s", wantErr: true},
		{name: "interval bajo el minimo", interval: "500ms", wantErr: true},
		{name: "interval basura", interval: "x", wantErr: true},
		{name: "interval mayor que down after por defecto", interval: "3m", wantErr: true},
		{name: "interval mayor que down after explicito", downAfter: "1m", interval: "2m", wantErr: true},
		{name: "interval igual a down after", downAfter: "1m", interval: "1m", wantDownAt: time.Minute, wantInterval: time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := map[string]string{
				"DATABASE_URL":              "postgres://x",
				"ARGOS_HOST_DOWN_AFTER":     tt.downAfter,
				"ARGOS_HOST_CHECK_INTERVAL": tt.interval,
			}
			cfg, err := Load(func(k string) string { return env[k] })
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if cfg.HostDownAfter != tt.wantDownAt || cfg.HostCheckInterval != tt.wantInterval {
				t.Errorf("got %s/%s, want %s/%s", cfg.HostDownAfter, cfg.HostCheckInterval, tt.wantDownAt, tt.wantInterval)
			}
		})
	}
}

func TestLoadMetricsRetention(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                         string
		raw, hourly, checks, every   string
		wantRaw, wantHourly, wantChk int
		wantEvery                    time.Duration
		wantErr                      bool
	}{
		{name: "defaults", wantRaw: 30, wantHourly: 400, wantChk: 14, wantEvery: 5 * time.Minute},
		{name: "valores validos", raw: "60", hourly: "365", checks: "20", every: "1m", wantRaw: 60, wantHourly: 365, wantChk: 20, wantEvery: time.Minute},
		{name: "minimos", raw: "8", hourly: "8", checks: "8", every: "5s", wantRaw: 8, wantHourly: 8, wantChk: 8, wantEvery: 5 * time.Second},
		{name: "crudo bajo el minimo", raw: "7", wantErr: true},
		{name: "crudo cero", raw: "0", wantErr: true},
		{name: "crudo negativo", raw: "-30", wantErr: true},
		{name: "crudo no numerico", raw: "un-mes", wantErr: true},
		{name: "crudo decimal", raw: "30.5", wantErr: true},
		{name: "horario bajo el minimo", hourly: "7", wantErr: true},
		{name: "horario menor que el crudo explicito", raw: "100", hourly: "50", wantErr: true},
		{name: "crudo mayor que el horario por defecto", raw: "500", wantErr: true},
		{name: "horario igual al crudo", raw: "100", hourly: "100", wantRaw: 100, wantHourly: 100, wantChk: 14, wantEvery: 5 * time.Minute},
		{name: "service checks bajo el minimo", checks: "7", wantErr: true},
		{name: "service checks basura", checks: "x", wantErr: true},
		{name: "intervalo bajo el minimo", every: "4s", wantErr: true},
		{name: "intervalo sin unidad", every: "300", wantErr: true},
		{name: "intervalo negativo", every: "-5m", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := map[string]string{
				"DATABASE_URL":                        "postgres://x",
				"ARGOS_METRICS_RETENTION_DAYS":        tt.raw,
				"ARGOS_METRICS_HOURLY_RETENTION_DAYS": tt.hourly,
				"ARGOS_SERVICE_CHECKS_RETENTION_DAYS": tt.checks,
				"ARGOS_METRICS_MAINTENANCE_INTERVAL":  tt.every,
			}
			cfg, err := Load(func(k string) string { return env[k] })
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if cfg.MetricsRetentionDays != tt.wantRaw || cfg.MetricsHourlyRetentionDays != tt.wantHourly ||
				cfg.ServiceChecksRetentionDays != tt.wantChk || cfg.MetricsMaintenanceInterval != tt.wantEvery {
				t.Errorf("got %d/%d/%d/%s, want %d/%d/%d/%s",
					cfg.MetricsRetentionDays, cfg.MetricsHourlyRetentionDays, cfg.ServiceChecksRetentionDays, cfg.MetricsMaintenanceInterval,
					tt.wantRaw, tt.wantHourly, tt.wantChk, tt.wantEvery)
			}
		})
	}
}

func TestLoadDiskRetention(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    int
		wantErr bool
	}{
		{name: "default", want: 30},
		{name: "valor valido", raw: "90", want: 90},
		{name: "minimo", raw: "8", want: 8},
		{name: "bajo el minimo", raw: "7", wantErr: true},
		{name: "cero", raw: "0", wantErr: true},
		{name: "negativo", raw: "-1", wantErr: true},
		{name: "no numerico", raw: "un-mes", wantErr: true},
		{name: "decimal", raw: "30.5", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := map[string]string{"DATABASE_URL": "postgres://x", "ARGOS_DISK_RETENTION_DAYS": tt.raw}
			cfg, err := Load(func(k string) string { return env[k] })
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && cfg.DiskRetentionDays != tt.want {
				t.Errorf("DiskRetentionDays got %d, want %d", cfg.DiskRetentionDays, tt.want)
			}
		})
	}
}

func TestLoadAlertSettings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     string
		raw     string
		get     func(Config) any
		want    any
		wantErr bool
	}{
		{name: "intervalo default", get: func(c Config) any { return c.AlertEvalInterval }, want: 15 * time.Second},
		{name: "intervalo valido", env: "ARGOS_ALERT_EVAL_INTERVAL", raw: "5s", get: func(c Config) any { return c.AlertEvalInterval }, want: 5 * time.Second},
		{name: "intervalo bajo el minimo", env: "ARGOS_ALERT_EVAL_INTERVAL", raw: "500ms", wantErr: true},
		{name: "intervalo invalido", env: "ARGOS_ALERT_EVAL_INTERVAL", raw: "rapido", wantErr: true},
		{name: "eventos default", get: func(c Config) any { return c.HostEventsRetentionDays }, want: 400},
		{name: "eventos minimo", env: "ARGOS_HOST_EVENTS_RETENTION_DAYS", raw: "30", get: func(c Config) any { return c.HostEventsRetentionDays }, want: 30},
		{name: "eventos bajo el minimo", env: "ARGOS_HOST_EVENTS_RETENTION_DAYS", raw: "29", wantErr: true},
		{name: "eventos de chequeos default", get: func(c Config) any { return c.ServiceEventsRetentionDays }, want: 180},
		{name: "eventos de chequeos minimo", env: "ARGOS_SERVICE_EVENTS_RETENTION_DAYS", raw: "30", get: func(c Config) any { return c.ServiceEventsRetentionDays }, want: 30},
		{name: "eventos de chequeos bajo el minimo", env: "ARGOS_SERVICE_EVENTS_RETENTION_DAYS", raw: "29", wantErr: true},
		{name: "inventario default", get: func(c Config) any { return c.InventoryRetentionDays }, want: 90},
		{name: "inventario minimo", env: "ARGOS_INVENTORY_RETENTION_DAYS", raw: "30", get: func(c Config) any { return c.InventoryRetentionDays }, want: 30},
		{name: "inventario bajo el minimo", env: "ARGOS_INVENTORY_RETENTION_DAYS", raw: "29", wantErr: true},
		{name: "alertas default", get: func(c Config) any { return c.AlertsRetentionDays }, want: 365},
		{name: "alertas valido", env: "ARGOS_ALERTS_RETENTION_DAYS", raw: "90", get: func(c Config) any { return c.AlertsRetentionDays }, want: 90},
		{name: "alertas bajo el minimo", env: "ARGOS_ALERTS_RETENTION_DAYS", raw: "7", wantErr: true},
		{name: "entregas default", get: func(c Config) any { return c.NotificationsRetentionDays }, want: 30},
		{name: "entregas minimo", env: "ARGOS_NOTIFICATIONS_RETENTION_DAYS", raw: "7", get: func(c Config) any { return c.NotificationsRetentionDays }, want: 7},
		{name: "entregas bajo el minimo", env: "ARGOS_NOTIFICATIONS_RETENTION_DAYS", raw: "6", wantErr: true},
		{name: "entregas no numerico", env: "ARGOS_NOTIFICATIONS_RETENTION_DAYS", raw: "un-mes", wantErr: true},
		{name: "notify default", get: func(c Config) any { return c.NotifyInterval }, want: 5 * time.Second},
		{name: "notify valido", env: "ARGOS_NOTIFY_INTERVAL", raw: "1s", get: func(c Config) any { return c.NotifyInterval }, want: time.Second},
		{name: "notify bajo el minimo", env: "ARGOS_NOTIFY_INTERVAL", raw: "200ms", wantErr: true},
		{name: "notify invalido", env: "ARGOS_NOTIFY_INTERVAL", raw: "seguido", wantErr: true},
		{name: "web dir", env: "ARGOS_WEB_DIR", raw: "/srv/web", get: func(c Config) any { return c.WebDir }, want: "/srv/web"},
		{name: "clave de secretos", env: "ARGOS_SECRET_KEY", raw: "abc", get: func(c Config) any { return c.SecretKey }, want: "abc"},
		{name: "allowlist de webhooks", env: "ARGOS_WEBHOOK_ALLOW_HOSTS", raw: "hooks.internal,10.0.0.0/8", get: func(c Config) any { return c.WebhookAllowHosts }, want: "hooks.internal,10.0.0.0/8"},
		{name: "CA de webhooks", env: "ARGOS_WEBHOOK_CA_FILE", raw: "/etc/ca.pem", get: func(c Config) any { return c.WebhookCAFile }, want: "/etc/ca.pem"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := map[string]string{"DATABASE_URL": "postgres://x"}
			if tt.env != "" {
				env[tt.env] = tt.raw
			}
			cfg, err := Load(func(k string) string { return env[k] })
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && tt.get(cfg) != tt.want {
				t.Errorf("got %v, want %v", tt.get(cfg), tt.want)
			}
		})
	}
}
