// Package checks ejecuta los chequeos de servicios (tcp, http, tls_cert, service, ping, snmp)
// que el servidor asigna al host. Los ejecuta solo el agente, con una politica
// local de destinos (ARGOS_CHECK_ALLOW) que el servidor no puede ampliar.
package checks

import (
	"log/slog"
	"strings"
)

// Tipos de chequeo (enumerados en codigo: un tipo desconocido da unknown/unsupported).
const (
	TypeTCP     = "tcp"
	TypeHTTP    = "http"
	TypeTLSCert = "tls_cert"
	TypeService = "service"
	TypePing    = "ping"
	TypeSNMP    = "snmp"
)

// Estados de un resultado.
const (
	StatusUp      = "up"
	StatusDown    = "down"
	StatusWarn    = "warn"
	StatusUnknown = "unknown"
)

// Catalogo fijo de detail: nunca IPs, URLs ni cuerpos.
const (
	DetailTimeout        = "timeout"
	DetailRefused        = "refused"
	DetailBlocked        = "blocked"
	DetailTLS            = "tls"
	DetailNotFound       = "not_found"
	DetailDenied         = "denied"
	DetailUnsupported    = "unsupported"
	DetailStatusMismatch = "status_mismatch"
	DetailTextMismatch   = "text_mismatch"
	DetailExpired        = "expired"
	DetailOK             = "ok"
	DetailAuthFailed     = "auth_failed"
	DetailNoSuchObject   = "no_such_object"
)

// Limites obligatorios del agente.
const (
	MaxChecks       = 20       // chequeos ejecutados por host; los demas dan unknown
	MinInterval     = 30       // segundos
	MaxConcurrent   = 4        // chequeos en vuelo a la vez
	MaxBodyBytes    = 64 << 10 // bytes de respuesta que se leen
	MaxTimeoutMS    = 10_000   // tope de timeout por intento
	minTimeoutMS    = 200      // piso del timeout
	defaultTimeout  = 5_000    // si el server manda 0
	maxRetries      = 3        // tope de reintentos inmediatos
	maxQueuedResult = 500      // resultados retenidos hasta el proximo reporte
	defaultExpect   = "200-399"
)

// Check es la definicion de un chequeo tal como la manda el servidor. Secret es
// un secreto: jamas se loguea ni se serializa de vuelta (ver LogValue).
type Check struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	IntervalS int    `json:"interval_s"`
	TimeoutMS int    `json:"timeout_ms"`
	Retries   int    `json:"retries"`

	TargetHost string `json:"target_host"` // tcp, tls_cert
	TargetPort int    `json:"target_port"` // tcp, tls_cert
	WarnMS     int    `json:"warn_ms"`     // tcp, http (0 = sin umbral)
	CritMS     int    `json:"crit_ms"`     // tcp, http (0 = sin umbral)

	URL          string `json:"url"`           // http
	ExpectStatus string `json:"expect_status"` // http: "200" o "200-399"
	ExpectText   string `json:"expect_text"`   // http
	HeaderName   string `json:"header_name"`   // http (allowlist)
	Secret       string `json:"secret"`        // http: valor de la cabecera

	WarnDays float64 `json:"warn_days"` // tls_cert
	CritDays float64 `json:"crit_days"` // tls_cert

	ServiceName string `json:"service_name"` // service

	Target   string `json:"target"`    // ping: IP literal (nunca hostname); usa warn_ms y crit_ms
	DeviceID string `json:"device_id"` // ping y snmp: solo informativo

	Metric  string     `json:"metric"`   // snmp: reachable | if_oper
	IfIndex int        `json:"if_index"` // snmp if_oper: 1-65535
	SNMP    SNMPParams `json:"snmp"`     // snmp: credenciales (secretas, ver SNMPParams)
}

// LogValue evita que un slog.Any(check) filtre el secreto.
func (c Check) LogValue() slog.Value {
	return slog.GroupValue(slog.String("id", c.ID), slog.String("type", c.Type))
}

// Set es la lista completa de chequeos del host con su revision.
type Set struct {
	Rev   string  `json:"rev"`
	Items []Check `json:"items"`
}

// Result es un resultado tal como va en `services` del reporte.
type Result struct {
	CheckID    string   `json:"check_id"`
	Name       string   `json:"name"`
	Status     string   `json:"status"`
	ResponseMS int64    `json:"response_ms"`
	Value      *float64 `json:"value"`
	Detail     string   `json:"detail"`
	TS         int64    `json:"ts"`
	// Trusted (solo tls_cert): la cadena valida contra las CAs del sistema. Campo extra opcional.
	Trusted *bool `json:"trusted,omitempty"`
}

// outcome es lo que mide un intento antes de ponerle id y ts.
type outcome struct {
	status  string
	detail  string
	ms      int64
	value   *float64
	trusted *bool
}

func unknown(detail string) outcome { return outcome{status: StatusUnknown, detail: detail} }

// validServiceName replica ^[A-Za-z0-9@._:-]{1,64}$ y ademas rechaza el guion inicial
// (evita que un nombre se lea como opcion de systemctl).
func validServiceName(s string) bool {
	if s == "" || len(s) > 64 || strings.HasPrefix(s, "-") {
		return false
	}
	for _, r := range s {
		ok := r == '@' || r == '.' || r == '_' || r == ':' || r == '-' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			return false
		}
	}
	return true
}
