package inventory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"runtime"
	"sync"
	"time"
)

const (
	inventoryPath = "/api/v1/agent/inventory"

	// minGap separa dos envios completos del mismo modulo.
	minGap = 30 * time.Second

	failBase = 30 * time.Second // backoff tras un fallo transitorio (red, 5xx, 429, 401)
	failMax  = 15 * time.Minute

	// Un 404/405 significa que el servidor no soporta inventario: se reintenta muy de vez en cuando.
	unsupportedBase = time.Hour
	unsupportedMax  = 24 * time.Hour

	// startJitter reparte el primer envio completo de puertos, servicios y procesos: sin esto una flota que
	// arranca junta (reinicio del servidor o despliegue) manda todos los inventarios a la vez. No retrasa las
	// metricas: van por otro camino (agent.Run) y el host aparece de inmediato.
	startJitter = 5 * time.Minute
	// periodSpread es la fraccion (+-) de jitter sobre el periodo de recoleccion de esos tres modulos.
	periodSpread = 0.1

	// El modulo de parches arranca con un retardo aleatorio (no todos los hosts a la vez).
	updatesStartJitter = 10 * time.Minute
	updatesHeartbeat   = 24 * time.Hour
	updatesSpread      = 0.2
)

// Options configura Run.
type Options struct {
	Endpoint   string       // URL base del servidor, sin path
	Token      string       // agent token: solo va en el header Authorization
	HTTPClient *http.Client // el cliente compartido del agente
	Modules    []string     // ya validados con ParseModules; vacio = no hace nada
	Logger     *slog.Logger
	Sources    *Sources // nil = las lecturas reales del SO
	Updates    UpdatesConfig
	// Jitter devuelve un valor uniforme en [0, d]; nil = aleatorio. Inyectable en tests.
	Jitter func(d time.Duration) time.Duration
}

// module es un colector con su cadencia.
type module struct {
	name      string
	interval  time.Duration // cada cuanto se recolecta
	heartbeat time.Duration // cada cuanto se refresca sin cambios
	collect   func(ctx context.Context) (snapshot, error)
	start     time.Duration // la primera recoleccion espera un aleatorio en [0, start]
	spread    float64       // fraccion de jitter (+-) sobre interval; 0 = fijo
}

func buildModule(name string, src Sources, ucfg UpdatesConfig) (module, bool) {
	switch name {
	case ModulePorts:
		return module{name: name, interval: time.Minute, heartbeat: 6 * time.Hour, collect: func(ctx context.Context) (snapshot, error) { return collectPorts(ctx, src) }, start: startJitter, spread: periodSpread}, true
	case ModuleServices:
		if src.Services == nil {
			return module{}, false
		}
		return module{name: name, interval: 10 * time.Minute, heartbeat: 6 * time.Hour, collect: func(ctx context.Context) (snapshot, error) { return collectServices(ctx, src) }, start: startJitter, spread: periodSpread}, true
	case ModuleProcesses:
		return module{name: name, interval: 5 * time.Minute, heartbeat: 30 * time.Minute, collect: func(ctx context.Context) (snapshot, error) { return collectProcesses(ctx, src) }, start: startJitter, spread: periodSpread}, true
	case ModuleUpdates:
		if src.Updates == nil {
			return module{}, false
		}
		interval := ucfg.Interval
		if interval == 0 {
			interval = DefaultUpdatesInterval
		}
		interval = min(max(interval, MinUpdatesInterval), MaxUpdatesInterval)
		if runtime.GOOS == "darwin" {
			interval = max(interval, MaxUpdatesInterval) // softwareupdate contacta a Apple: 1 vez al dia
		}
		return module{name: name, interval: interval, heartbeat: updatesHeartbeat, start: updatesStartJitter, spread: updatesSpread,
			collect: func(ctx context.Context) (snapshot, error) { return collectUpdates(ctx, src) }}, true
	}
	return module{}, false
}

// Run recolecta y envia los modulos pedidos hasta que ctx se cancele. Con Modules vacio
// devuelve de inmediato sin ejecutar nada. Cada modulo tiene su goroutine; Run espera a que
// terminen todas. Los fallos solo se loguean: el inventario nunca detiene al agente.
func Run(ctx context.Context, o Options) {
	if len(o.Modules) == 0 {
		return
	}
	logger := o.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	endpoint, err := url.JoinPath(o.Endpoint, inventoryPath)
	if err != nil {
		logger.Error("inventario desactivado: url invalida", "err", err)
		return
	}
	src := SystemSources()
	if o.Sources != nil {
		src = *o.Sources
	}
	if src.Updates == nil {
		src.Updates = systemUpdates(o.Updates)
	}
	cl := &client{url: endpoint, token: o.Token, http: o.HTTPClient}
	if cl.http == nil {
		cl.http = &http.Client{Timeout: 10 * time.Second}
	}

	var wg sync.WaitGroup
	for _, name := range o.Modules {
		m, ok := buildModule(name, src, o.Updates)
		if !ok {
			logger.Warn("modulo de inventario no disponible en este sistema, se omite", "module", name)
			continue
		}
		w := &worker{mod: m, cl: cl, log: logger.With("module", name), now: time.Now, rnd: o.Jitter}
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.loop(ctx)
		}()
	}
	wg.Wait()
}

// worker lleva el estado de envio de un modulo. Un solo dueno: su goroutine.
type worker struct {
	mod module
	cl  *client
	log *slog.Logger
	now func() time.Time
	rnd func(time.Duration) time.Duration // nil = jitter aleatorio

	sent     bool // ya hay un envio completo aceptado
	lastRev  string
	lastSend time.Time
	needFull bool // el servidor pidio el listado completo (409)

	unsupported int  // rachas de 404/405 seguidas
	failing     bool // ya se aviso del fallo actual
	failures    int  // fallos transitorios seguidos (pa el backoff)
}

func (w *worker) loop(ctx context.Context) {
	wait := w.jitter(w.mod.start)
	for {
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		wait = w.step(ctx)
	}
}

// every es la espera normal entre recolecciones, con jitter si el modulo lo pide.
func (w *worker) every() time.Duration {
	d := w.mod.interval
	if w.mod.spread <= 0 {
		return d
	}
	span := time.Duration(float64(d) * w.mod.spread)
	return d - span + w.jitter(2*span)
}

// jitter usa la fuente inyectada o, si no hay, la aleatoria.
func (w *worker) jitter(d time.Duration) time.Duration {
	if w.rnd != nil {
		return w.rnd(d)
	}
	return jitter(d)
}

// step hace un ciclo (recolectar, decidir, enviar) y devuelve cuanto esperar al siguiente.
func (w *worker) step(ctx context.Context) time.Duration {
	snap, err := w.mod.collect(ctx)
	if err != nil {
		if ctx.Err() == nil {
			w.log.Warn("no se pudo recolectar el inventario", "err", err)
		}
		return w.every()
	}
	now := w.now()
	full := !w.sent || w.needFull || snap.rev != w.lastRev
	switch {
	case full && w.sent && !w.needFull && now.Sub(w.lastSend) < minGap:
		return minGap - now.Sub(w.lastSend)
	case !full && now.Sub(w.lastSend) < w.mod.heartbeat:
		return w.every()
	}
	var body []byte
	if full {
		body, err = json.Marshal(fullBody{Module: w.mod.name, Rev: snap.rev, Items: snap.items, Truncated: snap.truncated, CollectedAt: now.UTC()})
	} else {
		body, err = json.Marshal(heartbeatBody{Module: w.mod.name, Rev: snap.rev})
	}
	if err != nil {
		w.log.Error("serializando el inventario", "err", err)
		return w.every()
	}
	status, err := w.cl.post(ctx, body)
	return w.handle(ctx, status, err, full, snap.rev, now)
}

// handle traduce la respuesta del servidor al estado del worker y al proximo intervalo.
func (w *worker) handle(ctx context.Context, status int, err error, wasFull bool, rev string, now time.Time) time.Duration {
	switch {
	case err != nil:
		if ctx.Err() != nil {
			return w.every()
		}
		return w.fail("error de red", err, "")
	case status >= 200 && status <= 299:
		if w.unsupported > 0 {
			w.log.Info("el servidor ya soporta inventario, modulo reactivado")
		}
		if w.failing {
			w.log.Info("envio de inventario recuperado")
		}
		w.sent, w.lastRev, w.lastSend, w.needFull = true, rev, now, false
		w.unsupported, w.failing = 0, false
		return w.every()
	case unsupportedStatus(status, w.mod.name == ModuleUpdates):
		w.unsupported++
		d := backoff(unsupportedBase, unsupportedMax, w.unsupported-1)
		if w.unsupported == 1 {
			w.log.Warn("el servidor no soporta inventario: modulo desactivado, se reintenta mas tarde", "status", status, "retry_in", d.Round(time.Minute).String())
		} else {
			w.log.Debug("el servidor sigue sin soportar inventario", "status", status)
		}
		return d
	case status == http.StatusConflict && !wasFull:
		// need_full: la foto del servidor no coincide con el latido; se reenvia completo ya.
		w.needFull = true
		return time.Second
	default:
		return w.fail("respuesta inesperada", nil, fmt.Sprint(status))
	}
}

// unsupportedStatus decide si la respuesta significa "el servidor no acepta este modulo": se desactiva con
// backoff largo en vez de reintentar a cada rato. Para los modulos de siempre (puertos, servicios, procesos)
// solo cuentan 404/405 (servidor viejo): un 400/413/422 es un fallo del envio y se reintenta como cualquier
// otro. `broad` (solo el modulo nuevo `updates`) amplia a cualquier 4xx no transitorio, porque un servidor que
// aun no conoce el modulo responde 400/422.
func unsupportedStatus(status int, broad bool) bool {
	if status < 400 || status > 499 {
		return false
	}
	if !broad {
		return status == http.StatusNotFound || status == http.StatusMethodNotAllowed
	}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusRequestTimeout, http.StatusConflict, http.StatusTooManyRequests:
		return false
	}
	return true
}

// fail cuenta un fallo transitorio: avisa solo en el primero de la racha y espera con backoff.
func (w *worker) fail(what string, err error, status string) time.Duration {
	w.unsupported = 0
	if !w.failing {
		w.log.Warn("no se pudo enviar el inventario, se reintenta con espera creciente", "motivo", what, "status", status, "err", err)
		w.failing = true
		w.failures = 0
	}
	d := backoff(failBase, failMax, w.failures)
	w.failures++
	return d
}

type fullBody struct {
	Module      string    `json:"module"`
	Rev         string    `json:"rev"`
	Items       any       `json:"items"`
	Truncated   bool      `json:"truncated"`
	CollectedAt time.Time `json:"collected_at"`
}

type heartbeatBody struct {
	Module string `json:"module"`
	Rev    string `json:"rev"`
}

// client hace el POST autenticado; comparte el http.Client del agente.
type client struct {
	url   string
	token string
	http  *http.Client
}

// post envia body y devuelve el status. No lee mas que un poco de la respuesta.
func (c *client) post(ctx context.Context, body []byte) (int, error) {
	if len(body) > maxBodyBytes {
		return 0, fmt.Errorf("cuerpo de %d bytes excede el tope de %d", len(body), maxBodyBytes)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("creando request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("enviando inventario: %w", err) // url.Error no incluye el token
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10)) // drenar (acotado) permite reusar la conexion
	return resp.StatusCode, nil
}

// backoff: base*2^n con tope max y jitter en [d/2, d].
func backoff(base, limit time.Duration, n int) time.Duration {
	d := base
	for i := 0; i < n && d < limit; i++ {
		d *= 2
	}
	d = min(d, limit)
	return d/2 + jitter(d/2)
}

// jitter devuelve una duracion aleatoria en [0, d].
func jitter(d time.Duration) time.Duration {
	//nolint:gosec // jitter de espera: no requiere aleatoriedad criptografica
	return time.Duration(rand.Int64N(int64(d) + 1))
}
