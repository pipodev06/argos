// Package sender envia reportes de metricas al servidor argos con reintentos
// acotados y un buffer offline persistente en disco (buffer.log en el directorio de estado).
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

	// DefaultDrainPace es el ritmo sostenido de envios (buffer y reporte en vivo). El server limita
	// las metricas a 1 req/2 s por host (rafaga 5): 2,1 s deja 5 % de margen pa el jitter de red.
	DefaultDrainPace = 2100 * time.Millisecond
	// drainBurst son los envios seguidos sin pausa tras un rato ocioso (de los 5 que aguanta el server;
	// quedan 2 de reserva pa el reporte en vivo y los reintentos).
	drainBurst = 3
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
	// PhysicalDisks agrupa los montajes por disco fisico; nil = el agente no lo sabe.
	PhysicalDisks []PhysicalDisk `json:"physical_disks,omitempty"`
}

// PhysicalDisk es un disco fisico con los montajes (los mismos `mount` de Disks) que aloja.
type PhysicalDisk struct {
	ID     string   `json:"id"`
	Model  string   `json:"model,omitempty"`
	Bus    string   `json:"bus,omitempty"`
	SizeMB int64    `json:"size_mb,omitempty"`
	Mounts []string `json:"mounts"`
}

func (d PhysicalDisk) equal(o PhysicalDisk) bool {
	return d.ID == o.ID && d.Model == o.Model && d.Bus == o.Bus && d.SizeMB == o.SizeMB && slices.Equal(d.Mounts, o.Mounts)
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
		h.AgentVersion == o.AgentVersion && h.Privilege == o.Privilege && slices.Equal(h.IPs, o.IPs) &&
		slices.EqualFunc(h.PhysicalDisks, o.PhysicalDisks, PhysicalDisk.equal)
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

	HTTPClient     *http.Client // default: Timeout 10s
	MaxBuffer      int          // reportes retenidos offline (default DefaultMaxBuffer)
	MaxBufferBytes int64        // bytes retenidos offline (default DefaultMaxBufferBytes)
	// Store persiste el buffer offline en el directorio de estado (nil = solo memoria).
	Store FileStore
	// MaxAge descarta reportes mas viejos que esto sin enviarlos: el server rechaza timestamps fuera
	// de su ventana (0 = no caducan).
	MaxAge      time.Duration
	Now         func() time.Time // default time.Now
	MaxAttempts int              // intentos por reporte en cada flush (default 3)
	BaseDelay   time.Duration    // backoff inicial (default 1s)
	MaxDelay    time.Duration    // tope del backoff (default 30s)
	DrainPace   time.Duration    // pausa entre envios al vaciar el buffer (default DefaultDrainPace)
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

	q      *queue
	tat    time.Time // instante teorico del proximo envio (ver waitTurn)
	maxAge time.Duration
	now    func() time.Time
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
		maxAge:      o.MaxAge,
		now:         o.Now,
	}
	if c.logger == nil {
		c.logger = slog.New(slog.DiscardHandler)
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: 10 * time.Second}
	}
	if c.maxBuffer <= 0 {
		c.maxBuffer = DefaultMaxBuffer
	}
	if c.now == nil {
		c.now = time.Now
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
	maxBytes := o.MaxBufferBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBufferBytes
	}
	c.q = newQueue(o.Store, c.maxBuffer, maxBytes, c.logger)
	c.q.load()
	return c, nil
}

// Buffered es la cantidad de reportes pendientes de envio.
func (c *Client) Buffered() int { return c.q.len() }

// Close libera el archivo del buffer offline. Lo pendiente queda en disco pa el proximo arranque.
func (c *Client) Close() { c.q.close() }

// Send encola el reporte y trata de vaciar el buffer en orden (el mas viejo primero),
// con una pausa DrainPace entre envios pa no superar el limite del server.
// Devuelve nil si no hubo error (el buffer puede quedar con reportes si se
// alcanzo el tope de envios por Send: salen en el proximo). Si falla por causa
// transitoria (red, 5xx, 429, 408) los reportes quedan en el buffer pa el proximo
// Send; si el servidor rechaza un reporte de forma definitiva (otro 4xx) ese
// reporte se descarta y se devuelve el error. Un 401 devuelve ErrTokenRejected
// sin descartar nada: el llamador debe detenerse.
func (c *Client) Send(ctx context.Context, r Report) error {
	c.q.push(r)
	return c.flush(ctx)
}

// dropExpired descarta los reportes del frente mas viejos que MaxAge: el server los rechazaria
// (ventana de timestamps) y reenviarlos solo gasta el limite de ritmo.
func (c *Client) dropExpired() {
	if c.maxAge <= 0 {
		return
	}
	limit := c.now().Add(-c.maxAge).Unix()
	n := 0
	for c.q.len() > 0 && c.q.head().Timestamp < limit {
		c.q.pop()
		n++
	}
	if n > 0 {
		c.logger.Warn("reportes offline demasiado viejos, se descartan sin enviar", "descartados", n)
	}
}

func (c *Client) flush(ctx context.Context) error {
	var dropped error
	for processed := 0; ; processed++ {
		c.dropExpired()
		if c.q.len() == 0 {
			break
		}
		if processed > 0 && c.maxPerSend > 0 && processed >= c.maxPerSend {
			c.logger.Info("buffer pendiente, sigue en el proximo ciclo", "pending", c.q.len())
			break
		}
		if err := c.waitTurn(ctx); err != nil {
			return err
		}
		err := c.sendWithRetry(ctx, *c.q.head())
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
		c.q.pop()
	}
	return dropped
}

// waitTurn reparte los envios al ritmo del limite del server (GCRA: un envio cada drainPace en promedio,
// con una rafaga de hasta drainBurst tras un rato ocioso). El estado (tat) persiste entre Sends, asi
// dos ciclos seguidos no suman rafagas. El tiempo es virtual (tat + espera), no depende de que Sleep avance el reloj.
func (c *Client) waitTurn(ctx context.Context) error {
	now := c.now()
	at := now
	if wait := c.tat.Add(-(drainBurst - 1) * c.drainPace).Sub(now); wait > 0 {
		if err := c.sleep(ctx, wait); err != nil {
			return err
		}
		at = now.Add(wait)
	}
	c.tat = later(c.tat, at).Add(c.drainPace)
	return nil
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
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
