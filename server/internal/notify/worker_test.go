package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"argos/server/internal/store"
)

func TestBackoffGrowsWithJitterAndIsCapped(t *testing.T) {
	t.Parallel()
	tests := []struct {
		attempts int
		jitter   float64
		want     time.Duration
	}{
		{1, 0, 30 * time.Second},
		{2, 0, time.Minute},
		{3, 0, 2 * time.Minute},
		{4, 0, 4 * time.Minute},
		{5, 0, 8 * time.Minute},
		{6, 0, 16 * time.Minute},
		{8, 0, time.Hour},  // 64 min -> tope
		{50, 0, time.Hour}, // sin desbordar
		{1, 0.2, 36 * time.Second},
		{1, -0.2, 24 * time.Second},
		{8, 0.2, time.Hour}, // el jitter no rompe el tope
		{0, 0, 30 * time.Second},
	}
	for _, tt := range tests {
		if got := Backoff(tt.attempts, tt.jitter); got != tt.want {
			t.Errorf("Backoff(%d, %v) got %v, want %v", tt.attempts, tt.jitter, got, tt.want)
		}
	}
}

// fakeQueue es una cola en memoria: ClaimDeliveries devuelve lo cargado una sola vez y registra los resultados.
type fakeQueue struct {
	mu       sync.Mutex
	enabled  bool
	jobs     []store.DeliveryJob
	claims   int
	usage    map[string]int // mensajes por hora por canal
	leaves   time.Duration
	sent     [][]int64
	sentCode int
	failed   []store.DeliveryFailure
	released [][]int64
	relWait  time.Duration
	suppress [][]int64
	claimErr error
}

func (q *fakeQueue) HasEnabledChannels(context.Context) (bool, error) { return q.enabled, nil }

func (q *fakeQueue) ClaimDeliveries(context.Context, int, time.Duration, time.Duration) ([]store.DeliveryJob, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.claims++
	jobs := q.jobs
	q.jobs = nil
	return jobs, q.claimErr
}

func (q *fakeQueue) MarkDeliveriesSent(_ context.Context, ids []int64, code int) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.sent, q.sentCode = append(q.sent, ids), code
	return nil
}

func (q *fakeQueue) MarkDeliveriesFailed(_ context.Context, f store.DeliveryFailure) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.failed = append(q.failed, f)
	return nil
}

func (q *fakeQueue) ReleaseDeliveries(_ context.Context, ids []int64, wait time.Duration, _ string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.released, q.relWait = append(q.released, ids), wait
	return nil
}

func (q *fakeQueue) SuppressDeliveries(_ context.Context, ids []int64, _ string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.suppress = append(q.suppress, ids)
	return nil
}

func (q *fakeQueue) ChannelHourUsage(_ context.Context, ch string) (int, time.Duration, error) {
	return q.usage[ch], q.leaves, nil
}

// fakeSender registra los mensajes enviados por destino.
type fakeSender struct {
	mu   sync.Mutex
	got  map[string][]byte // URL -> ultimo cuerpo
	n    map[string]int
	sec  map[string]string
	err  error
	code int
}

func (s *fakeSender) Send(_ context.Context, t Target, body []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.got == nil {
		s.got, s.n, s.sec = map[string][]byte{}, map[string]int{}, map[string]string{}
	}
	s.got[t.URL], s.sec[t.URL] = body, t.HMACSecret
	s.n[t.URL]++
	return s.code, s.err
}

func newTestWorker(q Queue, box *Box, s Sender) *Worker {
	w := NewWorker(q, box, s, slog.New(slog.NewTextHandler(io.Discard, nil)), "https://argos.example.com")
	w.jitter = func() float64 { return 0 }
	w.now = func() time.Time { return t0 }
	return w
}

// jobFor arma una entrega de canal ch cuyo secreto (URL y HMAC) esta cifrado con box.
func jobFor(t *testing.T, box *Box, id int64, ch, url, event, sev, host string, attempts int) store.DeliveryJob {
	t.Helper()
	raw, err := json.Marshal(WebhookSecret{URL: url, HMAC: "hmac-" + ch})
	if err != nil {
		t.Fatal(err)
	}
	blob, err := box.Seal(ch, KindWebhook, raw)
	if err != nil {
		t.Fatal(err)
	}
	kid := box.KeyID()
	return store.DeliveryJob{
		ID: id, AlertID: "alert-" + host, ChannelID: ch, Event: event, Attempts: attempts, CreatedAt: t0,
		Hostname: host, RuleName: "CPU alta", Severity: sev, TriggerValue: f64(95), Threshold: f64(90),
		ChannelName: ch, ChannelKind: KindWebhook, ChannelEnabled: true, SecretEnc: blob, SecretKeyID: &kid,
	}
}

func TestWorkerSendsOneGroupedMessagePerChannel(t *testing.T) {
	t.Parallel()
	box := mustBox(t, 1)
	q := &fakeQueue{enabled: true}
	q.jobs = []store.DeliveryJob{
		jobFor(t, box, 1, "A", "https://a.example.com/x", "opened", "warn", "h1", 1),
		jobFor(t, box, 2, "A", "https://a.example.com/x", "opened", "warn", "h2", 1),
		jobFor(t, box, 3, "A", "https://a.example.com/x", "resolved", "warn", "h3", 1),
		jobFor(t, box, 4, "B", "https://b.example.com/x", "opened", "critical", "h1", 1),
	}
	s := &fakeSender{code: 200}
	st, err := newTestWorker(q, box, s).RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.n["https://a.example.com/x"] != 1 || s.n["https://b.example.com/x"] != 1 {
		t.Errorf("mensajes por canal got %v, want 1 y 1", s.n)
	}
	var msg struct {
		Alerts []map[string]any `json:"alerts"`
	}
	if err := json.Unmarshal(s.got["https://a.example.com/x"], &msg); err != nil || len(msg.Alerts) != 3 {
		t.Errorf("el canal A debe llevar sus 3 alertas en un mensaje: %v, %d", err, len(msg.Alerts))
	}
	if s.sec["https://a.example.com/x"] != "hmac-A" || s.sec["https://b.example.com/x"] != "hmac-B" {
		t.Errorf("cada canal firma con su propio secreto: %v", s.sec)
	}
	if st.Claimed != 4 || st.Sent != 4 || len(q.sent) != 2 || q.sentCode != 200 {
		t.Errorf("stats %+v sent=%v code=%d", st, q.sent, q.sentCode)
	}
}

func TestWorkerDoesNothingWithoutEnabledChannels(t *testing.T) {
	t.Parallel()
	box := mustBox(t, 1)
	q := &fakeQueue{enabled: false, jobs: []store.DeliveryJob{jobFor(t, box, 1, "A", "https://a.example.com", "opened", "warn", "h", 1)}}
	st, err := newTestWorker(q, box, &fakeSender{}).RunOnce(context.Background())
	if err != nil || q.claims != 0 || st != (Stats{}) {
		t.Errorf("got %+v, %v, claims=%d; want ciclo vacio sin reclamar", st, err, q.claims)
	}
}

func TestWorkerFailureSchedulesBackoffAndSanitizesTheError(t *testing.T) {
	t.Parallel()
	box := mustBox(t, 1)
	const secretURL = "https://hooks.example.com/services/TOKEN-SECRETO"
	q := &fakeQueue{enabled: true}
	q.jobs = []store.DeliveryJob{
		jobFor(t, box, 1, "A", secretURL, "opened", "critical", "h1", 1),
		jobFor(t, box, 2, "A", secretURL, "opened", "critical", "h2", 3),
	}
	s := &fakeSender{code: 503, err: &SendError{Category: CatStatus, HTTPStatus: 503}}
	st, err := newTestWorker(q, box, s).RunOnce(context.Background())
	if err != nil || st.Failed != 2 || len(q.failed) != 1 {
		t.Fatalf("stats %+v err %v failed %d", st, err, len(q.failed))
	}
	f := q.failed[0]
	if f.Delays[0] != 30*time.Second || f.Delays[1] != 2*time.Minute {
		t.Errorf("backoff por intento got %v, want 30s y 2m", f.Delays)
	}
	if f.HTTPStatus == nil || *f.HTTPStatus != 503 || f.LastError != "respuesta http 503" {
		t.Errorf("fallo got %+v", f)
	}
	if len(f.LastError) > 300 {
		t.Error("last_error > 300")
	}
}

func TestWorkerNeverLeaksURLOrTransportDetailInLastError(t *testing.T) {
	t.Parallel()
	box := mustBox(t, 1)
	const secretURL = "https://hooks.example.com/services/TOKEN-SECRETO"
	q := &fakeQueue{enabled: true}
	q.jobs = []store.DeliveryJob{jobFor(t, box, 1, "A", secretURL, "opened", "critical", "h1", 1)}
	// Un error de transporte crudo (con la URL adentro) no debe llegar a last_error.
	s := &fakeSender{err: errors.New(`Post "` + secretURL + `": dial tcp 10.0.0.1:443: refused`)}
	if _, err := newTestWorker(q, box, s).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := q.failed[0].LastError; got != "fallo de envio" {
		t.Errorf("last_error got %q, want mensaje generico", got)
	}
}

func TestWorkerDefersEverythingWhenTheChannelHitItsHourlyLimit(t *testing.T) {
	t.Parallel()
	box := mustBox(t, 1)
	q := &fakeQueue{enabled: true, usage: map[string]int{"A": store.ChannelHourlyLimit}, leaves: 90 * time.Second}
	q.jobs = []store.DeliveryJob{
		jobFor(t, box, 1, "A", "https://a.example.com/x", "opened", "critical", "h1", 1),
		jobFor(t, box, 2, "A", "https://a.example.com/x", "opened", "critical", "h2", 1),
		jobFor(t, box, 3, "B", "https://b.example.com/x", "opened", "critical", "h1", 1),
	}
	s := &fakeSender{code: 200}
	st, err := newTestWorker(q, box, s).RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.n["https://a.example.com/x"] != 0 || s.n["https://b.example.com/x"] != 1 {
		t.Errorf("A no debe enviar, B si: %v", s.n)
	}
	if st.Released != 2 || st.Sent != 1 || q.relWait != 90*time.Second {
		t.Errorf("stats %+v espera %v, want 2 liberadas y 90s (cuando sale el mensaje mas viejo)", st, q.relWait)
	}
}

func TestWorkerCollapsesABurstIntoOneSummaryMessage(t *testing.T) {
	t.Parallel()
	box := mustBox(t, 1)
	q := &fakeQueue{enabled: true, usage: map[string]int{"A": store.ChannelHourlyLimit - 1}}
	for i := range 40 {
		q.jobs = append(q.jobs, jobFor(t, box, int64(i+1), "A", "https://a.example.com/x", "opened", "critical", "h", 1))
	}
	s := &fakeSender{code: 200}
	st, err := newTestWorker(q, box, s).RunOnce(context.Background())
	if err != nil || s.n["https://a.example.com/x"] != 1 || st.Sent != 40 {
		t.Fatalf("stats %+v err %v mensajes %v", st, err, s.n)
	}
	var msg struct {
		Alerts  []any `json:"alerts"`
		Omitted int   `json:"omitted"`
	}
	if err := json.Unmarshal(s.got["https://a.example.com/x"], &msg); err != nil || len(msg.Alerts) != MaxItemsPerMessage || msg.Omitted != 40-MaxItemsPerMessage {
		t.Errorf("resumen got %d alertas y %d omitidas (%v)", len(msg.Alerts), msg.Omitted, err)
	}
}

func TestWorkerSuppressesDisabledChannelsAndFailsWhenTheSecretCannotBeOpened(t *testing.T) {
	t.Parallel()
	box := mustBox(t, 1)
	disabled := jobFor(t, box, 1, "A", "https://a.example.com/x", "opened", "critical", "h", 1)
	disabled.ChannelEnabled = false
	broken := jobFor(t, box, 2, "B", "https://b.example.com/x", "opened", "critical", "h", 1)
	broken.SecretEnc[len(broken.SecretEnc)-1] ^= 0xff

	q := &fakeQueue{enabled: true, jobs: []store.DeliveryJob{disabled, broken}}
	s := &fakeSender{code: 200}
	st, err := newTestWorker(q, box, s).RunOnce(context.Background())
	if err != nil || st.Suppressed != 1 || st.Failed != 1 || len(s.n) != 0 {
		t.Fatalf("stats %+v err %v envios %v", st, err, s.n)
	}
	if q.failed[0].LastError != "no se pudo descifrar el secreto del canal" {
		t.Errorf("last_error got %q", q.failed[0].LastError)
	}
}

func TestWorkerFailsClearlyWithoutASecretKey(t *testing.T) {
	t.Parallel()
	box := mustBox(t, 1)
	q := &fakeQueue{enabled: true, jobs: []store.DeliveryJob{jobFor(t, box, 1, "A", "https://a.example.com/x", "opened", "critical", "h", 1)}}
	st, err := newTestWorker(q, nil, &fakeSender{}).RunOnce(context.Background())
	if err != nil || st.Failed != 1 || q.failed[0].LastError != "secreto del canal no disponible" {
		t.Errorf("stats %+v err %v failed %+v", st, err, q.failed)
	}
}

func TestWorkerLeavesLeaseAloneWhenShuttingDown(t *testing.T) {
	t.Parallel()
	box := mustBox(t, 1)
	q := &fakeQueue{enabled: true, jobs: []store.DeliveryJob{jobFor(t, box, 1, "A", "https://a.example.com/x", "opened", "critical", "h", 1)}}
	ctx, cancel := context.WithCancel(context.Background())
	s := &cancelingSender{cancel: cancel}
	st, err := newTestWorker(q, box, s).RunOnce(ctx)
	if err != nil || len(q.sent) != 0 || len(q.failed) != 0 || st.Sent+st.Failed != 0 {
		t.Errorf("con el apagado en curso no se registra nada (el lease vence solo): %+v %v", st, err)
	}
}

type cancelingSender struct{ cancel context.CancelFunc }

func (c *cancelingSender) Send(ctx context.Context, _ Target, _ []byte) (int, error) {
	c.cancel()
	return 0, ctx.Err()
}
