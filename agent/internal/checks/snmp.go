package checks

import (
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"strconv"
	"time"
)

// Limites del colector SNMP (fijos: el servidor no puede subirlos).
const (
	MaxSNMPConcurrent = 16               // dispositivos consultados a la vez
	MaxSNMPResponse   = 64 << 10         // bytes; una respuesta mayor se descarta
	snmpPort          = 161              // fijo: el servidor no elige el puerto
	snmpMaxTimeout    = 3 * time.Second  // tope de timeout por intento
	snmpRetries       = 1                // reintentos del wrapper
	snmpBreakerFails  = 3                // fallos seguidos que abren el breaker
	snmpBreakerBase   = 30 * time.Second // primera espera con el breaker abierto
	snmpBreakerMax    = 5 * time.Minute  // tope del backoff exponencial
	maxSNMPSecretLen  = 256
	maxSNMPUserLen    = 64

	oidSysUpTime     = "1.3.6.1.2.1.1.3.0"
	oidIfOperStatusP = "1.3.6.1.2.1.2.2.1.8." // + ifIndex

	metricReachable = "reachable"
	metricIfOper    = "if_oper"
)

// Errores del cliente SNMP (el wrapper traduce los de gosnmp a estos).
var (
	ErrSNMPTimeout      = errors.New("snmp: sin respuesta")
	ErrSNMPAuth         = errors.New("snmp: autenticacion rechazada")
	ErrSNMPNoSuchObject = errors.New("snmp: objeto inexistente")
	ErrSNMPTooLarge     = errors.New("snmp: respuesta demasiado grande")
)

// SNMPParams son las credenciales que el servidor entrega en el chequeo. Los campos
// de contrasena y comunidad son secretos: jamas se loguean ni se imprimen.
type SNMPParams struct {
	Version   int    `json:"version"` // 3 (authPriv) o 2 (v2c, solo si el servidor lo marca)
	User      string `json:"user"`
	Level     string `json:"level"`      // v3: solo authPriv
	AuthProto string `json:"auth_proto"` // SHA256 | SHA512
	AuthPass  string `json:"auth_pass"`
	PrivProto string `json:"priv_proto"` // AES128 | AES256
	PrivPass  string `json:"priv_pass"`
	Community string `json:"community"` // v2c
}

// LogValue y String evitan que slog o fmt filtren los secretos.
func (SNMPParams) LogValue() slog.Value { return slog.StringValue("[snmp]") }
func (SNMPParams) String() string       { return "[snmp]" }
func (SNMPParams) GoString() string     { return "[snmp]" }

// snmpRequest es una solicitud GET de un solo OID.
type snmpRequest struct {
	IP      netip.Addr
	Params  SNMPParams
	OID     string
	Timeout time.Duration // por intento
}

// SNMPGetter hace UN GET de un OID y devuelve su valor entero. Devuelve ErrSNMPTimeout,
// ErrSNMPAuth, ErrSNMPNoSuchObject, ErrSNMPTooLarge o cualquier otro error (dato inutilizable).
type SNMPGetter interface {
	Get(ctx context.Context, req snmpRequest) (int64, error)
}

// snmpOverallTimeout es el plazo total de un chequeo snmp: intento + reintentos del wrapper.
func snmpOverallTimeout(perAttempt time.Duration) time.Duration {
	return min(perAttempt, snmpMaxTimeout) * (1 + snmpRetries)
}

// validSNMPParams acepta solo v3 authPriv con SHA-256/512 + AES-128/256, o v2c con comunidad.
func validSNMPParams(p SNMPParams) bool {
	switch p.Version {
	case 3:
		return p.Level == "authPriv" && p.User != "" && len(p.User) <= maxSNMPUserLen &&
			(p.AuthProto == "SHA256" || p.AuthProto == "SHA512") &&
			(p.PrivProto == "AES128" || p.PrivProto == "AES256") &&
			validSecret(p.AuthPass) && validSecret(p.PrivPass)
	case 2:
		return validSecret(p.Community)
	default:
		return false
	}
}

func validSecret(s string) bool { return s != "" && len(s) <= maxSNMPSecretLen }

// snmpDevice es el estado por dispositivo: una solicitud a la vez y circuit breaker.
type snmpDevice struct {
	slot      chan struct{} // capacidad 1: una solicitud en vuelo por dispositivo
	fails     int           // fallos seguidos (protegido por Runner.snmpMu)
	openUntil time.Time
}

func (r *Runner) snmpDevice(ip netip.Addr) *snmpDevice {
	r.snmpMu.Lock()
	defer r.snmpMu.Unlock()
	d, ok := r.snmpDevs[ip]
	if !ok { // acotado: solo llegan IPs de la allowlist local
		d = &snmpDevice{slot: make(chan struct{}, 1)}
		r.snmpDevs[ip] = d
	}
	return d
}

// breakerOpen dice si el dispositivo esta en espera tras fallos seguidos.
func (r *Runner) breakerOpen(d *snmpDevice) bool {
	r.snmpMu.Lock()
	defer r.snmpMu.Unlock()
	return r.now().Before(d.openUntil)
}

// recordSNMP anota el resultado: un exito cierra el breaker; 3 fallos seguidos lo abren
// con espera 30 s, 60 s, 120 s... hasta 5 min.
func (r *Runner) recordSNMP(d *snmpDevice, failed bool) {
	r.snmpMu.Lock()
	defer r.snmpMu.Unlock()
	if !failed {
		d.fails, d.openUntil = 0, time.Time{}
		return
	}
	d.fails++
	if d.fails >= snmpBreakerFails {
		shift := min(d.fails-snmpBreakerFails, 10)
		d.openUntil = r.now().Add(min(snmpBreakerBase<<shift, snmpBreakerMax))
	}
}

// probeSNMP valida destino y argumentos y hace un GET (con el reintento del wrapper).
func (r *Runner) probeSNMP(ctx context.Context, c Check) outcome {
	ip, err := netip.ParseAddr(c.Target)
	if err != nil || ip.Zone() != "" {
		return unknown(DetailUnsupported)
	}
	ip = ip.Unmap()
	if !r.probeAllow.Allows(ip) {
		return unknown(DetailBlocked)
	}
	oid, ok := snmpOID(c)
	if !ok || !validSNMPParams(c.SNMP) || r.snmp == nil {
		return unknown(DetailUnsupported)
	}
	d := r.snmpDevice(ip)
	select {
	case d.slot <- struct{}{}:
		defer func() { <-d.slot }()
	case <-ctx.Done():
		return unknown(DetailTimeout)
	}
	if r.breakerOpen(d) {
		return outcome{status: StatusDown, detail: DetailTimeout}
	}
	start := r.now()
	v, err := r.snmp.Get(ctx, snmpRequest{IP: ip, Params: c.SNMP, OID: oid, Timeout: min(timeoutOf(c), snmpMaxTimeout)})
	ms := r.now().Sub(start).Milliseconds()
	switch {
	case err == nil:
		r.recordSNMP(d, false)
		return snmpValueOutcome(c.Metric, v, ms)
	case errors.Is(err, ErrSNMPTimeout):
		r.recordSNMP(d, true)
		return outcome{status: StatusDown, detail: DetailTimeout, ms: ms}
	case errors.Is(err, ErrSNMPAuth):
		r.recordSNMP(d, true)
		return outcome{status: StatusUnknown, detail: DetailAuthFailed, ms: ms}
	case errors.Is(err, ErrSNMPNoSuchObject):
		r.recordSNMP(d, false) // el dispositivo respondio: el breaker se cierra
		return outcome{status: StatusUnknown, detail: DetailNoSuchObject, ms: ms}
	default: // incluye ErrSNMPTooLarge y respuestas mal formadas
		r.recordSNMP(d, true)
		return outcome{status: StatusUnknown, detail: DetailBadResponse, ms: ms}
	}
}

// snmpOID devuelve el OID fijo de la metrica (sin OIDs libres); false si los argumentos no valen.
func snmpOID(c Check) (string, bool) {
	switch c.Metric {
	case metricReachable:
		return oidSysUpTime, c.IfIndex == 0
	case metricIfOper:
		if c.IfIndex < 1 || c.IfIndex > 65535 {
			return "", false
		}
		return oidIfOperStatusP + strconv.Itoa(c.IfIndex), true
	default:
		return "", false
	}
}

// snmpValueOutcome: reachable = uptime en segundos (TimeTicks/100); if_oper: 1 up, 2 down, otro warn.
func snmpValueOutcome(metric string, v int64, ms int64) outcome {
	if metric == metricReachable {
		val := float64(v) / 100
		return outcome{status: StatusUp, detail: DetailOK, ms: ms, value: &val}
	}
	val := float64(v)
	st := StatusWarn
	switch v {
	case 1:
		st = StatusUp
	case 2:
		st = StatusDown
	}
	return outcome{status: st, detail: DetailOK, ms: ms, value: &val}
}
