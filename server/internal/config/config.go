// Package config carga y valida la configuracion del servidor desde variables de entorno.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultListenAddr     = "127.0.0.1:8080"
	defaultAuditRetention = 180
	minAuditRetention     = 30 // el trigger de audit_log solo deja purgar filas de mas de 30 dias

	defaultHostDownAfter     = 2 * time.Minute
	minHostDownAfter         = 30 * time.Second // el intervalo por defecto del agente es 30 s
	defaultHostCheckInterval = 15 * time.Second
	minHostCheckInterval     = time.Second

	// Retencion de metricas. El minimo de 8 dias cubre los 7 dias que el ingest acepta hacia
	// atras (timestamp >= now-7d): ninguna muestra aceptada cae en una particion ya borrada.
	defaultMetricsRetentionDays       = 30
	defaultMetricsHourlyRetentionDays = 400
	defaultServiceChecksRetentionDays = 14
	defaultDiskRetentionDays          = 30
	minRawRetentionDays               = 8

	defaultMetricsMaintenanceInterval = 5 * time.Minute
	minMetricsMaintenanceInterval     = 5 * time.Second

	// Alertas: intervalo del evaluador y retencion de sus tablas.
	defaultAlertEvalInterval          = 15 * time.Second
	defaultNotifyInterval             = 5 * time.Second
	minNotifyInterval                 = time.Second
	minAlertEvalInterval              = time.Second
	defaultHostEventsRetentionDays    = 180
	defaultAlertsRetentionDays        = 365
	defaultNotificationsRetentionDays = 30
	defaultServiceEventsRetentionDays = 180
	minHostEventsRetentionDays        = 30
	minAlertsRetentionDays            = 30
	minNotificationsRetentionDays     = 7
	minServiceEventsRetentionDays     = 30
)

// Config es la configuracion ya validada del servidor.
type Config struct {
	// DatabaseURL es la URL de conexion a Postgres. Contiene credenciales: no se loguea.
	DatabaseURL string
	// ListenAddr es host:puerto donde escucha el servidor HTTP.
	ListenAddr string
	// PublicURL es la URL con la que los usuarios abren el dashboard (esquema://host[:puerto]).
	// Nil = solo localhost. Alimenta la allowlist de Host y HSTS (si es https).
	PublicURL *url.URL
	// TrustedProxies son los proxies inversos de confianza: solo de ellos se lee X-Forwarded-For.
	TrustedProxies []netip.Prefix
	// TLSCert y TLSKey (ambos o ninguno) activan HTTPS directo en el server.
	TLSCert, TLSKey string
	// AuditRetentionDays es cuanto se conserva audit_log (>= 30).
	AuditRetentionDays int
	// HostDownAfter es cuanto sin reportar hace que un host up pase a down (>= 30 s).
	HostDownAfter time.Duration
	// HostCheckInterval es cada cuanto corre el detector de hosts caidos (1 s .. HostDownAfter).
	HostCheckInterval time.Duration
	// MetricsRetentionDays es cuanto se conservan las metricas crudas (>= 8).
	MetricsRetentionDays int
	// MetricsHourlyRetentionDays es cuanto se conservan los resumenes por hora (>= MetricsRetentionDays).
	MetricsHourlyRetentionDays int
	// ServiceChecksRetentionDays es cuanto se conservan los chequeos de servicio (>= 8).
	ServiceChecksRetentionDays int
	// DiskRetentionDays es cuanto se conserva el uso de disco por montaje (disk_samples, >= 8).
	DiskRetentionDays int
	// MetricsMaintenanceInterval es cada cuanto corre el mantenimiento de particiones y resumenes (>= 5 s).
	MetricsMaintenanceInterval time.Duration
	// NotifyInterval es cada cuanto el worker de notificaciones drena la cola de entregas (>= 1 s).
	NotifyInterval time.Duration
	// SecretKey es ARGOS_SECRET_KEY (base64 de 32 bytes): cifra los secretos de los canales. Es un secreto: no se
	// loguea. Vacia = no hay canales con secreto (el arranque la exige si ya existen filas cifradas).
	SecretKey string
	// WebhookAllowHosts es ARGOS_WEBHOOK_ALLOW_HOSTS: hostnames, IPs o CIDRs a los que un webhook puede apuntar
	// aunque sean privados o usen otro puerto que 443. Solo se decide aqui, nunca por la API.
	WebhookAllowHosts string
	// WebhookCAFile es ARGOS_WEBHOOK_CA_FILE: PEM con CAs extra pa verificar los destinos webhook (CA interna).
	WebhookCAFile string
	// AlertEvalInterval es cada cuanto corre el evaluador de alertas (>= 1 s).
	AlertEvalInterval time.Duration
	// HostEventsRetentionDays es cuanto se conserva el historial de transiciones de status de los hosts (>= 30).
	HostEventsRetentionDays int
	// AlertsRetentionDays es cuanto se conservan las alertas resueltas y las ventanas de mantenimiento terminadas (>= 30).
	AlertsRetentionDays int
	// NotificationsRetentionDays es cuanto se conservan las entregas de notificaciones terminadas (>= 7).
	NotificationsRetentionDays int
	// ServiceEventsRetentionDays es cuanto se conserva el historial de transiciones de chequeos de servicio (>= 30).
	ServiceEventsRetentionDays int
	// AgentDistDir es el directorio con los binarios del agente que se publican en /dl (vacio = no se sirven).
	AgentDistDir string
	// InsecureCookies quita el prefijo __Host- y el atributo Secure de la cookie de sesion.
	// SOLO desarrollo sobre http (p. ej. Safari en localhost); nunca en produccion.
	InsecureCookies bool
}

// Load lee la configuracion usando getenv (os.Getenv en produccion) y falla
// con un mensaje claro si falta algo o es invalido.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		DatabaseURL:        getenv("DATABASE_URL"),
		ListenAddr:         getenv("ARGOS_LISTEN_ADDR"),
		TLSCert:            getenv("ARGOS_TLS_CERT"),
		TLSKey:             getenv("ARGOS_TLS_KEY"),
		AgentDistDir:       getenv("ARGOS_AGENT_DIST_DIR"),
		AuditRetentionDays: defaultAuditRetention,
		HostDownAfter:      defaultHostDownAfter,
		HostCheckInterval:  defaultHostCheckInterval,

		MetricsRetentionDays:       defaultMetricsRetentionDays,
		MetricsHourlyRetentionDays: defaultMetricsHourlyRetentionDays,
		ServiceChecksRetentionDays: defaultServiceChecksRetentionDays,
		DiskRetentionDays:          defaultDiskRetentionDays,
		MetricsMaintenanceInterval: defaultMetricsMaintenanceInterval,

		AlertEvalInterval:          defaultAlertEvalInterval,
		NotifyInterval:             defaultNotifyInterval,
		SecretKey:                  getenv("ARGOS_SECRET_KEY"),
		WebhookAllowHosts:          getenv("ARGOS_WEBHOOK_ALLOW_HOSTS"),
		WebhookCAFile:              getenv("ARGOS_WEBHOOK_CA_FILE"),
		HostEventsRetentionDays:    defaultHostEventsRetentionDays,
		AlertsRetentionDays:        defaultAlertsRetentionDays,
		NotificationsRetentionDays: defaultNotificationsRetentionDays,
		ServiceEventsRetentionDays: defaultServiceEventsRetentionDays,
	}

	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL es obligatoria (ver server/.env.example)")
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = defaultListenAddr
	}
	if err := validateListenAddr(cfg.ListenAddr); err != nil {
		return Config{}, fmt.Errorf("ARGOS_LISTEN_ADDR invalida: %w", err)
	}

	if raw := getenv("ARGOS_PUBLIC_URL"); raw != "" {
		u, err := parsePublicURL(raw)
		if err != nil {
			return Config{}, fmt.Errorf("ARGOS_PUBLIC_URL invalida: %w", err)
		}
		cfg.PublicURL = u
	}

	proxies, err := parseTrustedProxies(getenv("ARGOS_TRUSTED_PROXIES"))
	if err != nil {
		return Config{}, fmt.Errorf("ARGOS_TRUSTED_PROXIES invalida: %w", err)
	}
	cfg.TrustedProxies = proxies

	if (cfg.TLSCert == "") != (cfg.TLSKey == "") {
		return Config{}, errors.New("ARGOS_TLS_CERT y ARGOS_TLS_KEY deben definirse juntas")
	}

	if raw := getenv("ARGOS_AUDIT_RETENTION_DAYS"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < minAuditRetention {
			return Config{}, fmt.Errorf("ARGOS_AUDIT_RETENTION_DAYS debe ser un entero >= %d", minAuditRetention)
		}
		cfg.AuditRetentionDays = n
	}

	if raw := getenv("ARGOS_HOST_DOWN_AFTER"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d < minHostDownAfter {
			return Config{}, fmt.Errorf("ARGOS_HOST_DOWN_AFTER debe ser una duracion (p. ej. 2m) >= %s", minHostDownAfter)
		}
		cfg.HostDownAfter = d
	}
	if raw := getenv("ARGOS_HOST_CHECK_INTERVAL"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d < minHostCheckInterval {
			return Config{}, fmt.Errorf("ARGOS_HOST_CHECK_INTERVAL debe ser una duracion (p. ej. 15s) >= %s", minHostCheckInterval)
		}
		cfg.HostCheckInterval = d
	}
	if cfg.HostCheckInterval > cfg.HostDownAfter {
		return Config{}, fmt.Errorf("ARGOS_HOST_CHECK_INTERVAL (%s) no puede superar ARGOS_HOST_DOWN_AFTER (%s)", cfg.HostCheckInterval, cfg.HostDownAfter)
	}

	if err := loadRetention(getenv, &cfg); err != nil {
		return Config{}, err
	}

	if raw := getenv("ARGOS_INSECURE_COOKIES"); raw != "" {
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, errors.New("ARGOS_INSECURE_COOKIES debe ser true o false")
		}
		cfg.InsecureCookies = b
	}
	return cfg, nil
}

// loadRetention lee y valida la retencion de metricas y el intervalo de mantenimiento.
func loadRetention(getenv func(string) string, cfg *Config) error {
	ints := []struct {
		env string
		dst *int
		min int
	}{
		{"ARGOS_METRICS_RETENTION_DAYS", &cfg.MetricsRetentionDays, minRawRetentionDays},
		{"ARGOS_METRICS_HOURLY_RETENTION_DAYS", &cfg.MetricsHourlyRetentionDays, minRawRetentionDays},
		{"ARGOS_SERVICE_CHECKS_RETENTION_DAYS", &cfg.ServiceChecksRetentionDays, minRawRetentionDays},
		{"ARGOS_DISK_RETENTION_DAYS", &cfg.DiskRetentionDays, minRawRetentionDays},
		{"ARGOS_HOST_EVENTS_RETENTION_DAYS", &cfg.HostEventsRetentionDays, minHostEventsRetentionDays},
		{"ARGOS_ALERTS_RETENTION_DAYS", &cfg.AlertsRetentionDays, minAlertsRetentionDays},
		{"ARGOS_NOTIFICATIONS_RETENTION_DAYS", &cfg.NotificationsRetentionDays, minNotificationsRetentionDays},
		{"ARGOS_SERVICE_EVENTS_RETENTION_DAYS", &cfg.ServiceEventsRetentionDays, minServiceEventsRetentionDays},
	}
	for _, v := range ints {
		raw := getenv(v.env)
		if raw == "" {
			continue
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < v.min {
			return fmt.Errorf("%s debe ser un entero >= %d", v.env, v.min)
		}
		*v.dst = n
	}
	if cfg.MetricsHourlyRetentionDays < cfg.MetricsRetentionDays {
		return fmt.Errorf("ARGOS_METRICS_HOURLY_RETENTION_DAYS (%d) no puede ser menor que ARGOS_METRICS_RETENTION_DAYS (%d)",
			cfg.MetricsHourlyRetentionDays, cfg.MetricsRetentionDays)
	}

	if raw := getenv("ARGOS_METRICS_MAINTENANCE_INTERVAL"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d < minMetricsMaintenanceInterval {
			return fmt.Errorf("ARGOS_METRICS_MAINTENANCE_INTERVAL debe ser una duracion (p. ej. 5m) >= %s", minMetricsMaintenanceInterval)
		}
		cfg.MetricsMaintenanceInterval = d
	}

	if raw := getenv("ARGOS_ALERT_EVAL_INTERVAL"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d < minAlertEvalInterval {
			return fmt.Errorf("ARGOS_ALERT_EVAL_INTERVAL debe ser una duracion (p. ej. 15s) >= %s", minAlertEvalInterval)
		}
		cfg.AlertEvalInterval = d
	}

	if raw := getenv("ARGOS_NOTIFY_INTERVAL"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d < minNotifyInterval {
			return fmt.Errorf("ARGOS_NOTIFY_INTERVAL debe ser una duracion (p. ej. 5s) >= %s", minNotifyInterval)
		}
		cfg.NotifyInterval = d
	}
	return nil
}

func validateListenAddr(addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("se espera host:puerto (%w)", err)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("puerto %q fuera de rango 1-65535", port)
	}
	return nil
}

func parsePublicURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("no es una URL (%w)", err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, errors.New("se espera http(s)://host[:puerto]")
	}
	if strings.Trim(u.Path, "/") != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return nil, errors.New("no debe llevar ruta, query, fragmento ni credenciales")
	}
	return u, nil
}

// parseTrustedProxies acepta una lista separada por comas de IPs o CIDRs.
func parseTrustedProxies(raw string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if p, err := netip.ParsePrefix(part); err == nil {
			out = append(out, p.Masked())
			continue
		}
		addr, err := netip.ParseAddr(part)
		if err != nil {
			return nil, fmt.Errorf("%q no es una IP ni un CIDR", part)
		}
		out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return out, nil
}
