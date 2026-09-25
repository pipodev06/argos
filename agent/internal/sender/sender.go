// Package sender envia reportes de metricas al servidor argos con reintentos
// acotados y un buffer offline en memoria.
package sender

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"argos/agent/internal/checks"
)

const (
	metricsPath = "/api/v1/agent/metrics"

	// DefaultDrainPace separa los envios al vaciar el buffer. El server limita
	// las metricas a 1 req/2 s por host (rafaga 5): 2,5 s deja margen pa el jitter de red.
	DefaultDrainPace = 2500 * time.Millisecond
	// maxRetryAfter acota cuanto se le obedece a un Retry-After (un server roto no debe dormirnos horas).
	maxRetryAfter = 5 * time.Minute
	// maxResponseBytes acota la respuesta que se lee (la lista de chequeos ronda 15 KiB).
	maxResponseBytes = 64 << 10
)

// Metrics son las metricas del host en un instante. Los bloques opcionales
// (Swap, Load, Battery) son punteros: nil = "sin dato" y el campo se omite del
// JSON; jamas se manda un cero inventado.
type Metrics struct {
	CPUPct     float64         `json:"cpu_pct"`
	MemUsedMB  int             `json:"mem_used_mb"`
	MemTotalMB int             `json:"mem_total_mb"`
	Net        NetMetrics      `json:"net"`
	Swap       *SwapMetrics    `json:"swap,omitempty"`
	Load       *LoadMetrics    `json:"load,omitempty"`
	Battery    *BatteryMetrics `json:"battery,omitempty"`
}

// NetMetrics son contadores acumulados (desde el arranque del SO) de todas las
// interfaces que no son loopback; el server guarda el valor tal cual.
type NetMetrics struct {
	RxBytes int64 `json:"rx_bytes"`
	TxBytes int64 `json:"tx_bytes"`
}

// SwapMetrics es la memoria de intercambio en MiB (en Windows, el archivo de paginacion).
type SwapMetrics struct {
	UsedMB  int64 `json:"used_mb"`
	TotalMB int64 `json:"total_mb"`
}

// LoadMetrics es el load average de 1, 5 y 15 minutos (solo Linux/macOS).
type LoadMetrics struct {
	L1  float64 `json:"l1"`
	L5  float64 `json:"l5"`
	L15 float64 `json:"l15"`
}

// BatteryMetrics es el estado de la bateria. Charging es true si el equipo esta
// conectado a corriente (cargando o ya llena) y false si funciona con bateria.
type BatteryMetrics struct {
	Pct      int  `json:"pct"`
	Charging bool `json:"charging"`
}

// DiskUsage es el uso de un punto de montaje en MiB.
type DiskUsage struct {
	Mount   string `json:"mount"`
	UsedMB  int64  `json:"used_mb"`
	TotalMB int64  `json:"total_mb"`
}

// HostInfo describe el host. Los campos sin dato (cero o vacio) se omiten.
type HostInfo struct {
	BootTime      int64    `json:"boot_time,omitempty"` // epoch en segundos
	CPUModel      string   `json:"cpu_model,omitempty"`
	CPUCores      int      `json:"cpu_cores,omitempty"`
	CPUThreads    int      `json:"cpu_threads,omitempty"`
	OSName        string   `json:"os_name,omitempty"`
	OSVersion     string   `json:"os_version,omitempty"`
	KernelVersion string   `json:"kernel_version,omitempty"`
	AgentVersion  string   `json:"agent_version,omitempty"`
	Privilege     string   `json:"privilege,omitempty"` // "elevated" | "user"; vacio = no se pudo determinar
	IPs           []string `json:"ips,omitempty"`
}

// bootTimeTolerance: gopsutil calcula boot_time como ahora - uptime, asi que dos
// lecturas del mismo arranque pueden diferir unos segundos.
const bootTimeTolerance = 60 // segundos

// Equal dice si dos HostInfo describen lo mismo (boot_time con tolerancia).
// Las IPs se comparan en orden: el colector las entrega ordenadas.
func (h HostInfo) Equal(o HostInfo) bool {
	d := h.BootTime - o.BootTime
	if d < -bootTimeTolerance || d > bootTimeTolerance {
		return false
	}
	return h.CPUModel == o.CPUModel && h.CPUCores == o.CPUCores && h.CPUThreads == o.CPUThreads &&
		h.OSName == o.OSName && h.OSVersion == o.OSVersion && h.KernelVersion == o.KernelVersion &&
		h.AgentVersion == o.AgentVersion && h.Privilege == o.Privilege && slices.Equal(h.IPs, o.IPs)
}

// ErrTokenRejected: el servidor respondio 401, el agent token es invalido o fue
// revocado. Reintentar no sirve: hay que re-enrolar el agente.
var ErrTokenRejected = errors.New("token rechazado por el servidor: re-enrola el agente")

// Report es el payload de POST /api/v1/agent/metrics. No lleva el token:
// el servidor identifica al agente por el header Authorization.
//
// Disks y HostInfo van solo en algunos reportes (el primero y luego cada cierto
// tiempo, o cuando algo cambia); en el resto estan ausentes.
type Report struct {
	Hostname     string      `json:"hostname"`
	AgentVersion string      `json:"agent_version"`
	Timestamp    int64       `json:"timestamp"`
	Metrics      Metrics     `json:"metrics"`
	Disks        []DiskUsage `json:"disks,omitempty"`
	HostInfo     *HostInfo   `json:"host_info,omitempty"`
	// ChecksRev es la revision de la lista de chequeos que tiene el agente ("" = ninguna);
	// si difiere de la del servidor, la respuesta trae la lista nueva.
	ChecksRev string `json:"checks_rev,omitempty"`
	// Services son los resultados de chequeos desde el reporte anterior.
	Services []checks.Result `json:"services,omitempty"`
}

// inheritExtras pasa a r los datos ocasionales (discos, host_info) de un reporte
// que se va a descartar, salvo que r ya traiga los suyos (mas recientes). Asi un
// desborde del buffer no pierde un envio que solo ocurre cada minutos u horas.
func (r *Report) inheritExtras(from Report) {
	if r.Disks == nil {
		r.Disks = from.Disks
	}
	if r.HostInfo == nil {
		r.HostInfo = from.HostInfo
	}
}

// StatusError es una respuesta HTTP no exitosa del servidor.
type StatusError struct {
	Code int
	// RetryAfter es lo que pidio el servidor en la cabecera Retry-After (0 = no vino o no es valida).
	RetryAfter time.Duration
}

func (e *StatusError) Error() string { return fmt.Sprintf("el servidor respondio %d", e.Code) }

// Options configura el Client. Los campos con cero usan el default indicado.
type Options struct {
	Endpoint string // URL base del servidor, sin path
	Token    string // agent token: solo va en el header Authorization
	Logger   *slog.Logger

	HTTPClient  *http.Client  // default: Timeout 10s
	MaxBuffer   int           // reportes retenidos offline (default 120)
	MaxAttempts int           // intentos por reporte en cada flush (default 3)
	BaseDelay   time.Duration // backoff inicial (default 1s)
	MaxDelay    time.Duration // tope del backoff (default 30s)
	DrainPace   time.Duration // pausa entre envios al vaciar el buffer (default DefaultDrainPace)
	// OnChecks recibe la lista de chequeos que el servidor manda en la respuesta (nil = se ignora).
	OnChecks func(context.Context, checks.Set)
	Interval time.Duration                              // cadencia del agente: limita los envios por Send a Interval/DrainPace (0 = sin limite)
	Sleep    func(context.Context, time.Duration) error // espera entre reintentos; inyectable en tests
}

// Client envia reportes. NO es seguro pa uso concurrente: un solo dueno (el loop del agente).
type Client struct {
	url         string
	token       string
	logger      *slog.Logger
	http        *http.Client
	maxBuffer   int
	maxAttempts int
	baseDelay   time.Duration
	maxDelay    time.Duration
	drainPace   time.Duration
	maxPerSend  int // 0 = sin limite
	onChecks    func(context.Context, checks.Set)
	sleep       func(context.Context, time.Duration) error

	buf []Report
}

// New crea un Client. Falla si la URL base es invalida.
func New(o Options) (*Client, error) {
	endpoint, err := url.JoinPath(o.Endpoint, metricsPath)
	if err != nil {
		return nil, fmt.Errorf("construyendo url del endpoint: %w", err)
	}
	c := &Client{
		url:         endpoint,
		token:       o.Token,
		logger:      o.Logger,
		http:        o.HTTPClient,
		maxBuffer:   o.MaxBuffer,
		maxAttempts: o.MaxAttempts,
		baseDelay:   o.BaseDelay,
		maxDelay:    o.MaxDelay,
		drainPace:   o.DrainPace,
		sleep:       o.Sleep,
		onChecks:    o.OnChecks,
	}
	if c.logger == nil {
		c.logger = slog.New(slog.DiscardHandler)
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: 10 * time.Second}
	}
	if c.maxBuffer <= 0 {
		c.maxBuffer = 120
	}
	if c.maxAttempts <= 0 {
		c.maxAttempts = 3
	}
	if c.baseDelay <= 0 {
		c.baseDelay = time.Second
	}
	if c.maxDelay <= 0 {
		c.maxDelay = 30 * time.Second
	}
	if c.drainPace <= 0 {
		c.drainPace = DefaultDrainPace
	}
	if o.Interval > 0 {
		// Un Send no debe bloquear el loop mas de un intervalo: el resto del buffer
		// sale en los ticks siguientes, siempre a ritmo <= al limite del server.
		c.maxPerSend = max(1, int(o.Interval/c.drainPace))
	}
	if c.sleep == nil {
		c.sleep = sleepCtx
	}
	return c, nil
}

// Buffered es la cantidad de reportes pendientes de envio.
func (c *Client) Buffered() int { return len(c.buf) }

// Send encola el reporte y trata de vaciar el buffer en orden (el mas viejo primero),
// con una pausa DrainPace entre envios pa no superar el limite del server.
// Devuelve nil si no hubo error (el buffer puede quedar con reportes si se
// alcanzo el tope de envios por Send: salen en el proximo). Si falla por causa
// transitoria (red, 5xx, 429, 408) los reportes quedan en el buffer pa el proximo
// Send; si el servidor rechaza un reporte de forma definitiva (otro 4xx) ese
// reporte se descarta y se devuelve el error. Un 401 devuelve ErrTokenRejected
// sin descartar nada: el llamador debe detenerse.
func (c *Client) Send(ctx context.Context, r Report) error {
	if len(c.buf) >= c.maxBuffer {
		c.logger.Warn("buffer offline lleno, se descarta el reporte mas viejo", "max", c.maxBuffer)
		oldest := c.buf[0]
		c.buf = c.buf[1:]
		if len(c.buf) > 0 {
			c.buf[0].inheritExtras(oldest)
		} else {
			r.inheritExtras(oldest)
		}
	}
	c.buf = append(c.buf, r)
	return c.flush(ctx)
}

func (c *Client) flush(ctx context.Context) error {
	var dropped error
	for processed := 0; len(c.buf) > 0; processed++ {
		if processed > 0 {
			if c.maxPerSend > 0 && processed >= c.maxPerSend {
				c.logger.Info("buffer pendiente, sigue en el proximo ciclo", "pending", len(c.buf))
				break
			}
			if err := c.sleep(ctx, c.drainPace); err != nil {
				return err
			}
		}
		err := c.sendWithRetry(ctx, c.buf[0])
		switch {
		case err == nil:
		case errors.Is(err, ErrTokenRejected):
			return err
		case retryable(err) || ctx.Err() != nil:
			return err
		default:
			c.logger.Error("el servidor rechazo el reporte, se descarta", "err", err)
			dropped = err
		}
		c.buf = c.buf[1:]
	}
	return dropped
}

func (c *Client) sendWithRetry(ctx context.Context, r Report) error {
	var err error
	for attempt := 0; attempt < c.maxAttempts; attempt++ {
		if err = c.sendOnce(ctx, r); err == nil || !retryable(err) {
			return err
		}
		if attempt == c.maxAttempts-1 {
			break
		}
		delay := c.backoff(attempt)
		var se *StatusError
		if errors.As(err, &se) && se.RetryAfter > 0 {
			delay = se.RetryAfter // el servidor sabe cuanto falta: se le obedece en vez del backoff
		}
		c.logger.Warn("envio fallido, se reintenta", "attempt", attempt+1, "delay", delay, "err", err)
		if sleepErr := c.sleep(ctx, delay); sleepErr != nil {
			return sleepErr
		}
	}
	return err
}

func (c *Client) sendOnce(ctx context.Context, r Report) error {
	body, err := json.Marshal(r)
	if err != nil {
		return &permanentError{fmt.Errorf("serializando reporte: %w", err)}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return &permanentError{fmt.Errorf("creando request: %w", err)}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		// url.Error incluye la URL (sin token): es seguro loguearlo.
		return fmt.Errorf("enviando reporte: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return ErrTokenRejected
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10)) // drenar (acotado) permite reusar la conexion
		return &StatusError{Code: resp.StatusCode, RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())}
	}
	c.handleResponse(ctx, resp.Body)
	return nil
}

// handleResponse aplica la lista de chequeos que venga en data.checks. Un servidor viejo
// no la manda y una respuesta ilegible se ignora: el reporte ya fue aceptado.
func (c *Client) handleResponse(ctx context.Context, body io.Reader) {
	data, err := io.ReadAll(io.LimitReader(body, maxResponseBytes))
	if err != nil {
		c.logger.Debug("no se pudo leer la respuesta del servidor", "err", err)
		return
	}
	if c.onChecks == nil || len(data) == 0 {
		return
	}
	var env struct {
		Data struct {
			Checks *checks.Set `json:"checks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		c.logger.Debug("respuesta del servidor sin formato esperado, se ignora")
		return
	}
	if env.Data.Checks != nil {
		c.onChecks(ctx, *env.Data.Checks)
	}
}

// parseRetryAfter entiende segundos enteros o fecha HTTP; 0 si falta o es invalida. Se acota a maxRetryAfter.
func parseRetryAfter(v string, now time.Time) time.Duration {
	if v == "" {
		return 0
	}
	var d time.Duration
	if secs, err := strconv.ParseInt(v, 10, 32); err == nil {
		d = time.Duration(secs) * time.Second
	} else if t, err := http.ParseTime(v); err == nil {
		d = t.Sub(now)
	}
	return min(max(d, 0), maxRetryAfter)
}

// retryable: errores de red y 5xx/429/408 se reintentan; el resto de 4xx no.
func retryable(err error) bool {
	if errors.Is(err, ErrTokenRejected) {
		return false
	}
	var se *StatusError
	if errors.As(err, &se) {
		return se.Code >= 500 || se.Code == http.StatusTooManyRequests || se.Code == http.StatusRequestTimeout
	}
	var pe *permanentError
	if errors.As(err, &pe) {
		return false
	}
	// Cualquier otro error viene del transporte (red caida, timeout, DNS): transitorio,
	// salvo que el contexto se haya cancelado (apagado del agente).
	return !errors.Is(err, context.Canceled)
}

// permanentError marca fallos locales que reintentar no arregla (payload o request mal formados).
type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

// backoff: exponencial (base*2^attempt, tope maxDelay) con jitter en [d/2, d].
func (c *Client) backoff(attempt int) time.Duration {
	d := c.baseDelay
	for i := 0; i < attempt && d < c.maxDelay; i++ {
		d *= 2
	}
	d = min(d, c.maxDelay)
	half := d / 2
	//nolint:gosec // jitter de reintentos: no requiere aleatoriedad criptografica
	return half + time.Duration(rand.Int64N(int64(half)+1))
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("espera interrumpida: %w", ctx.Err())
	case <-t.C:
		return nil
	}
}
