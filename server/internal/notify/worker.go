package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"argos/server/internal/store"
)

// Parametros del worker (decididos en el diseno de alertas).
const (
	claimBatch   = 50
	leaseFor     = 60 * time.Second
	warnDelay    = 30 * time.Second // warn/info esperan pa agruparse; critical va de inmediato
	backoffBase  = 30 * time.Second
	backoffCap   = time.Hour
	jitterSpread = 0.2
	markTimeout  = 5 * time.Second
	maxParallel  = 8
	minRelease   = 5 * time.Second

	// KindWebhook es el unico tipo de canal con envio implementado.
	KindWebhook = "webhook"
)

// WebhookSecret es el contenido cifrado de un canal webhook: la URL (puede llevar un token) y el secreto HMAC.
type WebhookSecret struct {
	URL  string `json:"url"`
	HMAC string `json:"hmac,omitempty"`
}

// Queue es lo que el worker necesita del store.
type Queue interface {
	HasEnabledChannels(ctx context.Context) (bool, error)
	ClaimDeliveries(ctx context.Context, limit int, warnDelay, lease time.Duration) ([]store.DeliveryJob, error)
	MarkDeliveriesSent(ctx context.Context, ids []int64, httpStatus int) error
	MarkDeliveriesFailed(ctx context.Context, f store.DeliveryFailure) error
	ReleaseDeliveries(ctx context.Context, ids []int64, wait time.Duration, reason string) error
	SuppressDeliveries(ctx context.Context, ids []int64, reason string) error
	ChannelHourUsage(ctx context.Context, channelID string) (sent int, oldestLeavesIn time.Duration, err error)
}

// Sender envia un mensaje a un destino. Lo implementa *Client.
type Sender interface {
	Send(ctx context.Context, t Target, body []byte) (httpStatus int, err error)
}

// Backoff es la espera tras el intento numero attempts (1 = el primero): 30 s x 2^(n-1), con jitter (fraccion en
// [-0.2, 0.2]) y tope de 1 h.
func Backoff(attempts int, jitter float64) time.Duration {
	d := backoffBase
	for i := 1; i < attempts && d < backoffCap; i++ {
		d *= 2
	}
	d = time.Duration(float64(d) * (1 + jitter))
	return min(d, backoffCap)
}

// Stats resume un ciclo del worker.
type Stats struct {
	Claimed, Sent, Failed, Released, Suppressed int // entregas (no mensajes)
}

// Worker drena la cola de entregas: una vez por ciclo reclama las vencidas, las agrupa por canal y manda un
// mensaje por canal.
type Worker struct {
	q       Queue
	box     *Box // nil si no hay clave: los canales con secreto fallan con un error claro
	sender  Sender
	logger  *slog.Logger
	baseURL string
	now     func() time.Time
	jitter  func() float64
}

// NewWorker crea el worker. baseURL es ARGOS_PUBLIC_URL sin barra final ("" = sin enlaces).
func NewWorker(q Queue, box *Box, sender Sender, logger *slog.Logger, baseURL string) *Worker {
	return &Worker{
		q: q, box: box, sender: sender, logger: logger, baseURL: baseURL, now: time.Now,
		jitter: func() float64 { return (rand.Float64()*2 - 1) * jitterSpread }, //nolint:gosec // jitter de reintentos, no es un secreto
	}
}

// Run corre hasta que ctx se cancele. Cada interval hace un ciclo; sin canales habilitados no hace nada.
func (w *Worker) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	w.logger.Info("worker de notificaciones activo", "interval", interval.String())
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			st, err := w.RunOnce(ctx)
			if errors.Is(err, context.Canceled) {
				return
			}
			if err != nil {
				w.logger.Error("fallo el ciclo de notificaciones", "err", err)
				continue
			}
			if st.Claimed > 0 {
				w.logger.Info("notificaciones procesadas", "claimed", st.Claimed, "sent", st.Sent,
					"failed", st.Failed, "released", st.Released, "suppressed", st.Suppressed)
			}
		}
	}
}

// RunOnce hace un ciclo: reclama, agrupa por canal y envia (hasta maxParallel canales a la vez). Espera a que
// terminen todas las goroutines que lanzo.
func (w *Worker) RunOnce(ctx context.Context) (Stats, error) {
	has, err := w.q.HasEnabledChannels(ctx)
	if err != nil || !has {
		return Stats{}, err
	}
	jobs, err := w.q.ClaimDeliveries(ctx, claimBatch, warnDelay, leaseFor)
	if err != nil || len(jobs) == 0 {
		return Stats{}, err
	}

	var groups [][]store.DeliveryJob // jobs viene ordenado por canal
	for _, j := range jobs {
		if n := len(groups); n > 0 && groups[n-1][0].ChannelID == j.ChannelID {
			groups[n-1] = append(groups[n-1], j)
			continue
		}
		groups = append(groups, []store.DeliveryJob{j})
	}

	var (
		mu  sync.Mutex
		st  = Stats{Claimed: len(jobs)}
		wg  sync.WaitGroup
		sem = make(chan struct{}, maxParallel)
	)
	for _, g := range groups {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			outcome := w.processChannel(ctx, g)
			mu.Lock()
			defer mu.Unlock()
			switch outcome {
			case outcomeSent:
				st.Sent += len(g)
			case outcomeFailed:
				st.Failed += len(g)
			case outcomeReleased:
				st.Released += len(g)
			case outcomeSuppressed:
				st.Suppressed += len(g)
			}
		})
	}
	wg.Wait()
	return st, nil
}

type outcome int

const (
	outcomeNone outcome = iota // sin resultado registrado (apagado en curso): el lease vence y se reclama de nuevo
	outcomeSent
	outcomeFailed
	outcomeReleased
	outcomeSuppressed
)

// processChannel envia un mensaje con todas las entregas reclamadas de un canal.
func (w *Worker) processChannel(ctx context.Context, jobs []store.DeliveryJob) outcome {
	ids := make([]int64, len(jobs))
	for i, j := range jobs {
		ids[i] = j.ID
	}
	first := jobs[0]
	// Los resultados se escriben con un contexto propio: un apagado no debe perder lo que ya se envio.
	mctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), markTimeout)
	defer cancel()

	if !first.ChannelEnabled || first.ChannelKind != KindWebhook {
		return w.record(w.q.SuppressDeliveries(mctx, ids, "canal deshabilitado o tipo sin envio"), outcomeSuppressed, first.ChannelID)
	}

	sent, leavesIn, err := w.q.ChannelHourUsage(ctx, first.ChannelID)
	if err != nil {
		w.logger.Error("no se pudo contar los mensajes del canal", "channel_id", first.ChannelID, "err", err)
		return w.record(w.q.ReleaseDeliveries(mctx, ids, minRelease, "error interno, se reintenta"), outcomeReleased, first.ChannelID)
	}
	if sent >= store.ChannelHourlyLimit {
		wait := min(max(leavesIn, minRelease), backoffCap)
		return w.record(w.q.ReleaseDeliveries(mctx, ids, wait, "limite de mensajes por hora del canal"), outcomeReleased, first.ChannelID)
	}

	status, sendErr := w.send(ctx, jobs)
	if ctx.Err() != nil {
		return outcomeNone
	}
	if sendErr == nil {
		return w.record(w.q.MarkDeliveriesSent(mctx, ids, status), outcomeSent, first.ChannelID)
	}
	f := store.DeliveryFailure{IDs: ids, Delays: make([]time.Duration, len(jobs)), LastError: sendErr.Error()}
	for i, j := range jobs {
		f.Delays[i] = Backoff(j.Attempts, w.jitter())
	}
	if status != 0 {
		f.HTTPStatus = &status
	}
	return w.record(w.q.MarkDeliveriesFailed(mctx, f), outcomeFailed, first.ChannelID)
}

// record devuelve o si el resultado se registro; si no, lo loguea y devuelve outcomeNone (la entrega queda `sending` y el
// lease vencido la recupera).
func (w *Worker) record(err error, o outcome, channelID string) outcome {
	if err != nil {
		w.logger.Error("no se pudo registrar el resultado de las entregas", "channel_id", channelID, "err", err)
		return outcomeNone
	}
	return o
}

// send descifra el destino, arma el mensaje agrupado y lo envia. El error devuelto ya esta saneado (categoria
// generica: nunca URL, IP ni cuerpo).
func (w *Worker) send(ctx context.Context, jobs []store.DeliveryJob) (int, error) {
	first := jobs[0]
	if w.box == nil || first.SecretKeyID == nil || len(first.SecretEnc) == 0 {
		return 0, errors.New("secreto del canal no disponible")
	}
	raw, err := w.box.Open(first.ChannelID, *first.SecretKeyID, first.ChannelKind, first.SecretEnc)
	if err != nil {
		return 0, errors.New("no se pudo descifrar el secreto del canal")
	}
	var sec WebhookSecret
	if err := json.Unmarshal(raw, &sec); err != nil || sec.URL == "" {
		return 0, errors.New("secreto del canal invalido")
	}
	items := make([]Item, len(jobs))
	for i, j := range jobs {
		items[i] = Item{AlertID: j.AlertID, Event: j.Event, Hostname: j.Hostname, Rule: j.RuleName, Kind: j.RuleKind,
			Severity: j.Severity, Value: j.TriggerValue, Threshold: j.Threshold, Detail: deref(j.Detail)}
	}
	body, err := BuildMessage(items, 0, w.baseURL, w.now())
	if err != nil {
		return 0, fmt.Errorf("armando el mensaje: %w", err)
	}
	status, err := w.sender.Send(ctx, Target{URL: sec.URL, HMACSecret: sec.HMAC}, body)
	var se *SendError
	if err != nil && !errors.As(err, &se) {
		return status, errors.New("fallo de envio")
	}
	return status, err
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
