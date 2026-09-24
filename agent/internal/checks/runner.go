package checks

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"log/slog"
	"net/http"
	"net/netip"
	"sync"
	"time"
)

// Options configura el Runner. Los campos vacios usan el valor real; el resto se
// inyecta en tests.
type Options struct {
	Policy *Policy // nil = defecto (loopback + IPs propias)
	Logger *slog.Logger

	Now         func() time.Time
	MinInterval time.Duration // default MinInterval (30 s)
	Roots       *x509.CertPool
	Resolve     func(ctx context.Context, host string) ([]netip.Addr, error)
	OwnIPs      func() []netip.Addr
	Service     ServiceProber
}

// job es un chequeo en ejecucion periodica.
type job struct {
	check  Check
	cancel context.CancelFunc
}

// Runner planifica cada chequeo con su intervalo y junta los resultados hasta que el
// reporte los recoge. La lista no se persiste: al reiniciar arranca sin revision.
// Es seguro pa uso concurrente. Cada goroutine cuelga del ctx de Apply y termina al
// cancelarlo, al quitarse el chequeo o en Close.
type Runner struct {
	logger      *slog.Logger
	now         func() time.Time
	minInterval time.Duration
	roots       *x509.CertPool
	dialer      *dialer
	client      *http.Client
	service     ServiceProber
	sem         chan struct{}

	wg sync.WaitGroup

	mu      sync.Mutex
	rev     string
	jobs    map[string]*job
	results []Result
}

// NewRunner arma un Runner sin chequeos.
func NewRunner(o Options) *Runner {
	r := &Runner{
		logger:      o.Logger,
		now:         o.Now,
		minInterval: o.MinInterval,
		roots:       o.Roots,
		service:     o.Service,
		sem:         make(chan struct{}, MaxConcurrent),
		jobs:        map[string]*job{},
	}
	if r.logger == nil {
		r.logger = slog.New(slog.DiscardHandler)
	}
	if r.now == nil {
		r.now = time.Now
	}
	if r.minInterval <= 0 {
		r.minInterval = MinInterval * time.Second
	}
	if r.service == nil {
		r.service = newServiceProber()
	}
	d := &dialer{policy: o.Policy, resolve: o.Resolve, own: o.OwnIPs}
	if d.resolve == nil {
		d.resolve = defaultResolve
	}
	if d.own == nil {
		d.own = ownIPs
	}
	r.dialer = d
	r.client = &http.Client{
		Transport: &http.Transport{
			Proxy:                  nil,
			DialContext:            d.DialContext,
			TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: o.Roots},
			DisableKeepAlives:      true,
			MaxResponseHeaderBytes: MaxBodyBytes,
		},
		// Los redirects no se siguen: 3xx se evalua como respuesta.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return r
}

// Rev es la revision de la lista aplicada ("" = nunca se recibio una).
func (r *Runner) Rev() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rev
}

// Drain devuelve (y vacia) los resultados acumulados desde la llamada anterior.
func (r *Runner) Drain() []Result {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.results
	r.results = nil
	return out
}

// Close cancela todos los chequeos y espera a que terminen.
func (r *Runner) Close() {
	r.mu.Lock()
	for id, j := range r.jobs {
		j.cancel()
		delete(r.jobs, id)
	}
	r.mu.Unlock()
	r.wg.Wait()
}

// Apply reconcilia los chequeos en ejecucion con la lista del servidor: inicia los
// nuevos (primera ejecucion inmediata), reinicia los modificados y detiene los quitados.
// Con la misma revision no hace nada. ctx debe vivir tanto como el agente.
func (r *Runner) Apply(ctx context.Context, set Set) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if set.Rev != "" && set.Rev == r.rev {
		return
	}
	want := map[string]Check{}
	order := make([]string, 0, len(set.Items))
	for _, c := range set.Items {
		if _, dup := want[c.ID]; dup || c.ID == "" {
			continue
		}
		want[c.ID] = c
		order = append(order, c.ID)
	}
	for id, j := range r.jobs {
		if c, ok := want[id]; !ok || c != j.check {
			j.cancel()
			delete(r.jobs, id)
		}
	}
	if len(order) > MaxChecks {
		r.logger.Warn("el servidor asigno mas chequeos que el limite, los excedentes se reportan unknown",
			"assigned", len(order), "limit", MaxChecks)
	}
	for i, id := range order {
		if _, running := r.jobs[id]; running {
			continue
		}
		jctx, cancel := context.WithCancel(ctx)
		r.jobs[id] = &job{check: want[id], cancel: cancel}
		r.wg.Add(1)
		go r.loop(jctx, want[id], i >= MaxChecks)
	}
	r.rev = set.Rev
}

// effectiveInterval aplica el intervalo minimo.
func effectiveInterval(c Check, floor time.Duration) time.Duration {
	return max(time.Duration(c.IntervalS)*time.Second, floor)
}

func (r *Runner) loop(ctx context.Context, c Check, overLimit bool) {
	defer r.wg.Done()
	t := time.NewTicker(effectiveInterval(c, r.minInterval))
	defer t.Stop()
	for {
		r.runOnce(ctx, c, overLimit)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// runOnce ejecuta el chequeo (con reintentos) respetando la concurrencia maxima y
// deja el resultado en la cola. Un fallo solo afecta a este chequeo.
func (r *Runner) runOnce(ctx context.Context, c Check, overLimit bool) {
	var out outcome
	if overLimit {
		out = unknown(DetailUnsupported)
	} else {
		select {
		case r.sem <- struct{}{}:
			defer func() { <-r.sem }()
		case <-ctx.Done():
			return
		}
		out = r.attempts(ctx, c)
	}
	if ctx.Err() != nil {
		return // apagado o chequeo retirado: no se registra un resultado a medias
	}
	r.push(Result{
		CheckID: c.ID, Name: c.Name, Status: out.status, ResponseMS: out.ms,
		Value: out.value, Detail: out.detail, TS: r.now().Unix(), Trusted: out.trusted,
	})
}

// attempts hace el intento y hasta `retries` reintentos inmediatos mientras el
// resultado sea down; unknown (sin dato) no se reintenta ni se declara down.
func (r *Runner) attempts(ctx context.Context, c Check) outcome {
	tries := 1 + min(max(c.Retries, 0), maxRetries)
	var out outcome
	for i := 0; i < tries; i++ {
		out = r.probe(ctx, c)
		if out.status != StatusDown || ctx.Err() != nil {
			break
		}
	}
	return out
}

func (r *Runner) push(res Result) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.results) >= maxQueuedResult {
		r.results = r.results[1:]
	}
	r.results = append(r.results, res)
}

// running dice cuantos chequeos hay planificados (usado en tests).
func (r *Runner) running() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.jobs)
}
