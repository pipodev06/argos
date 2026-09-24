package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"
)

// Canales, asignacion a reglas y cola de entregas contra Postgres real. El tiempo lo pone now() de la BD: pa
// "avanzar" se retrasan a mano created_at / next_attempt_at / lease_until.

func newChan(name string) Channel {
	return Channel{Name: name, Kind: "webhook", Config: []byte(`{"host":"h"}`), MinSeverity: "warn", Enabled: true}
}

// mkChan crea un canal con id fijo derivado de n y un secreto opaco.
func mkChan(t *testing.T, s *Store, n int, name string) Channel {
	t.Helper()
	c := newChan(name)
	c.ID = fmt.Sprintf("00000000-0000-4000-8000-%012d", n)
	c.SecretEnc, c.SecretKeyID = []byte("blob-cifrado"), sptr("kid-1")
	got, err := s.CreateChannel(context.Background(), c, "")
	if err != nil {
		t.Fatalf("CreateChannel(%q): %v", name, err)
	}
	return got
}

func TestChannelCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := mkChan(t, s, 1, "Slack ops")
	if c.SecretKeyID == nil || *c.SecretKeyID != "kid-1" || string(c.SecretEnc) != "blob-cifrado" || !c.Enabled {
		t.Fatalf("canal creado got %+v", c)
	}
	dup := newChan("slack OPS")
	dup.ID = "00000000-0000-4000-8000-000000000002"
	if _, err := s.CreateChannel(ctx, dup, ""); !errors.Is(err, ErrChannelExists) {
		t.Errorf("nombre repetido (sin distinguir mayusculas) err got %v, want ErrChannelExists", err)
	}

	got, err := s.UpdateChannel(ctx, c.ID, func(ch *Channel) error {
		ch.Name, ch.MinSeverity, ch.SecretEnc = "Slack", "critical", []byte("otro-blob")
		return nil
	})
	if err != nil || got.Name != "Slack" || got.MinSeverity != "critical" || string(got.SecretEnc) != "otro-blob" {
		t.Fatalf("UpdateChannel got %+v, %v", got, err)
	}
	boom := errors.New("validacion")
	if _, err := s.UpdateChannel(ctx, c.ID, func(*Channel) error { return boom }); !errors.Is(err, boom) {
		t.Errorf("el error de mutate debe volver tal cual: %v", err)
	}
	if _, err := s.UpdateChannel(ctx, "00000000-0000-4000-8000-0000000000ff", func(*Channel) error { return nil }); !errors.Is(err, ErrNotFound) {
		t.Errorf("update inexistente err got %v, want ErrNotFound", err)
	}

	ids, err := s.ChannelKeyIDs(ctx)
	if err != nil || len(ids) != 1 || ids[0] != "kid-1" {
		t.Errorf("ChannelKeyIDs got %v, %v", ids, err)
	}
	if ok, err := s.HasEnabledChannels(ctx); err != nil || !ok {
		t.Errorf("HasEnabledChannels got %v, %v", ok, err)
	}

	name, kind, err := s.DeleteChannel(ctx, c.ID)
	if err != nil || name != "Slack" || kind != "webhook" {
		t.Errorf("DeleteChannel got %q %q %v", name, kind, err)
	}
	if _, _, err := s.DeleteChannel(ctx, c.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("borrar dos veces err got %v", err)
	}
	if ok, _ := s.HasEnabledChannels(ctx); ok {
		t.Error("sin canales no debe haber habilitados")
	}
	if ids, _ := s.ChannelKeyIDs(ctx); len(ids) != 0 {
		t.Errorf("sin canales no hay key ids: %v", ids)
	}
}

func TestChannelCap(t *testing.T) {
	s := newTestStore(t)
	for i := range MaxChannels {
		mkChan(t, s, i+1, fmt.Sprintf("c%d", i))
	}
	extra := newChan("uno mas")
	extra.ID = "00000000-0000-4000-8000-0000000000aa"
	if _, err := s.CreateChannel(context.Background(), extra, ""); !errors.Is(err, ErrTooManyChannels) {
		t.Errorf("err got %v, want ErrTooManyChannels", err)
	}
}

func TestSetRuleChannels(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	rule := mkRule(t, s, metricRule("cpu", "cpu_pct", "gt", 90, 0))
	a, b := mkChan(t, s, 1, "a"), mkChan(t, s, 2, "b")

	if got, _ := s.AlertRuleByID(ctx, rule.ID); len(got.ChannelIDs) != 0 || got.ChannelIDs == nil {
		t.Fatalf("una regla sin canales devuelve lista vacia (no nil): %#v", got.ChannelIDs)
	}
	if err := s.SetRuleChannels(ctx, rule.ID, []string{b.ID, a.ID}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.AlertRuleByID(ctx, rule.ID)
	if !slices.Equal(got.ChannelIDs, []string{a.ID, b.ID}) {
		t.Errorf("ChannelIDs got %v, want [%s %s]", got.ChannelIDs, a.ID, b.ID)
	}
	rules, _ := s.ListAlertRules(ctx)
	if len(rules) != 1 || len(rules[0].ChannelIDs) != 2 {
		t.Errorf("ListAlertRules debe traer los canales: %+v", rules)
	}

	// Reemplazo completo.
	if err := s.SetRuleChannels(ctx, rule.ID, []string{a.ID}); err != nil {
		t.Fatal(err)
	}
	// Un canal inexistente falla y deja la asignacion anterior intacta (transaccion).
	if err := s.SetRuleChannels(ctx, rule.ID, []string{b.ID, "00000000-0000-4000-8000-0000000000ff"}); !errors.Is(err, ErrChannelNotFound) {
		t.Errorf("canal inexistente err got %v, want ErrChannelNotFound", err)
	}
	if got, _ := s.AlertRuleByID(ctx, rule.ID); !slices.Equal(got.ChannelIDs, []string{a.ID}) {
		t.Errorf("tras el fallo debe quedar [a]: %v", got.ChannelIDs)
	}
	if err := s.SetRuleChannels(ctx, "00000000-0000-4000-8000-0000000000ff", nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("regla inexistente err got %v, want ErrNotFound", err)
	}
	if err := s.SetRuleChannels(ctx, rule.ID, make([]string, MaxChannelsPerRule+1)); !errors.Is(err, ErrTooManyRuleChannels) {
		t.Errorf("demasiados canales err got %v", err)
	}
	if err := s.SetRuleChannels(ctx, rule.ID, nil); err != nil {
		t.Fatal(err)
	}
	if n := count(t, `SELECT count(*) FROM rule_channels`); n != 0 {
		t.Errorf("rule_channels got %d, want 0", n)
	}

	// Borrar un canal lo saca de las reglas.
	if err := s.SetRuleChannels(ctx, rule.ID, []string{a.ID}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.DeleteChannel(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.AlertRuleByID(ctx, rule.ID); len(got.ChannelIDs) != 0 {
		t.Errorf("el canal borrado sigue en la regla: %v", got.ChannelIDs)
	}
}

// seedAlertDelivery crea una alerta de la severidad dada (host nuevo) y una entrega pending pa el canal, creada
// hace age.
func seedAlertDelivery(t *testing.T, ch, host, sev, event string, age time.Duration) int64 {
	t.Helper()
	seedHost(t, host, "up", ago(time.Second))
	var alertID string
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO alerts (host_id, rule_name, rule_kind, metric, threshold, severity, trigger_value, peak_value)
		 VALUES ($1, 'CPU alta', 'metric', 'cpu_pct', 90, $2, 95, 95) RETURNING id`, hostID(t, host), sev).Scan(&alertID); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO notification_deliveries (alert_id, channel_id, event, status, created_at, next_attempt_at)
		 VALUES ($1, $2, $3, 'pending', now() - $4::interval, now() - $4::interval) RETURNING id`,
		alertID, ch, event, fmt.Sprintf("%d seconds", int(age.Seconds()))).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func deliveryRow(t *testing.T, id int64) (status string, attempts int, lastErr *string, future bool) {
	t.Helper()
	if err := testPool.QueryRow(context.Background(),
		`SELECT status, attempts, last_error, next_attempt_at > now() FROM notification_deliveries WHERE id = $1`, id).
		Scan(&status, &attempts, &lastErr, &future); err != nil {
		t.Fatal(err)
	}
	return status, attempts, lastErr, future
}

func TestClaimDeliveriesTimingAndLease(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ch := mkChan(t, s, 1, "hook")

	crit := seedAlertDelivery(t, ch.ID, "h-crit", "critical", "opened", 0)
	freshWarn := seedAlertDelivery(t, ch.ID, "h-warn-new", "warn", "opened", 5*time.Second)
	oldWarn := seedAlertDelivery(t, ch.ID, "h-warn-old", "warn", "opened", 45*time.Second)

	jobs, err := s.ClaimDeliveries(ctx, 50, 30*time.Second, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var got []int64
	for _, j := range jobs {
		got = append(got, j.ID)
		if j.Attempts != 1 || j.ChannelID != ch.ID || j.Hostname == "" || j.RuleName != "CPU alta" || j.TriggerValue == nil || !j.ChannelEnabled ||
			string(j.SecretEnc) != "blob-cifrado" {
			t.Errorf("job incompleto: %+v", j)
		}
	}
	slices.Sort(got)
	if want := []int64{crit, oldWarn}; !slices.Equal(got, want) {
		t.Fatalf("reclamadas got %v, want %v (critical de inmediato, warn tras 30 s, la warn reciente espera)", got, want)
	}
	if st, _, _, _ := deliveryRow(t, freshWarn); st != "pending" {
		t.Errorf("la warn reciente debe seguir pending, got %s", st)
	}

	// Con el lease vigente nadie mas las reclama.
	if again, err := s.ClaimDeliveries(ctx, 50, 30*time.Second, time.Minute); err != nil || len(again) != 0 {
		t.Errorf("reclamo con lease vigente got %d, %v, want 0", len(again), err)
	}
	// Lease vencido (un worker que murio): se reclaman de nuevo y el intento cuenta.
	mustExec(t, `UPDATE notification_deliveries SET lease_until = now() - interval '1 second' WHERE id = $1`, crit)
	again, err := s.ClaimDeliveries(ctx, 50, 30*time.Second, time.Minute)
	if err != nil || len(again) != 1 || again[0].ID != crit || again[0].Attempts != 2 {
		t.Errorf("reclamo tras vencer el lease got %+v, %v, want la critical con attempts=2", again, err)
	}
}

func TestClaimDeliveriesConcurrentClaimersNeverOverlap(t *testing.T) {
	s := newTestStore(t)
	ch := mkChan(t, s, 1, "hook")
	const total = 40
	for i := range total {
		seedAlertDelivery(t, ch.ID, fmt.Sprintf("h%d", i), "critical", "opened", 0)
	}
	var (
		mu   sync.Mutex
		seen = map[int64]int{}
		wg   sync.WaitGroup
	)
	for range 8 {
		wg.Go(func() {
			jobs, err := s.ClaimDeliveries(context.Background(), 10, 30*time.Second, time.Minute)
			if err != nil {
				t.Errorf("ClaimDeliveries: %v", err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, j := range jobs {
				seen[j.ID]++
			}
		})
	}
	wg.Wait()
	for id, n := range seen {
		if n != 1 {
			t.Errorf("la entrega %d se reclamo %d veces", id, n)
		}
	}
	if len(seen) != total {
		t.Errorf("reclamadas %d, want %d", len(seen), total)
	}
}

func TestClaimRespectsTheLimit(t *testing.T) {
	s := newTestStore(t)
	ch := mkChan(t, s, 1, "hook")
	for i := range 7 {
		seedAlertDelivery(t, ch.ID, fmt.Sprintf("h%d", i), "critical", "opened", 0)
	}
	jobs, err := s.ClaimDeliveries(context.Background(), 5, 30*time.Second, time.Minute)
	if err != nil || len(jobs) != 5 {
		t.Errorf("got %d, %v, want 5", len(jobs), err)
	}
}

func TestFailedDeliveryBacksOffAndDiesAfterMaxAttempts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ch := mkChan(t, s, 1, "hook")
	id := seedAlertDelivery(t, ch.ID, "h", "critical", "opened", 0)

	for attempt := 1; attempt <= MaxDeliveryAttempts; attempt++ {
		jobs, err := s.ClaimDeliveries(ctx, 50, 30*time.Second, time.Minute)
		if err != nil || len(jobs) != 1 || jobs[0].Attempts != attempt {
			t.Fatalf("intento %d: reclamo got %+v, %v", attempt, jobs, err)
		}
		st := 503
		f := DeliveryFailure{IDs: []int64{id}, Delays: []time.Duration{time.Duration(attempt) * 30 * time.Second}, LastError: "respuesta http 503", HTTPStatus: &st}
		if err := s.MarkDeliveriesFailed(ctx, f); err != nil {
			t.Fatal(err)
		}
		status, n, lastErr, future := deliveryRow(t, id)
		if attempt < MaxDeliveryAttempts {
			if status != "pending" || !future || n != attempt {
				t.Fatalf("intento %d: got %s attempts=%d futuro=%v, want pending con backoff", attempt, status, n, future)
			}
			// Con el backoff vigente no se reclama.
			if again, _ := s.ClaimDeliveries(ctx, 50, 30*time.Second, time.Minute); len(again) != 0 {
				t.Fatalf("intento %d: se reclamo antes de tiempo", attempt)
			}
			mustExec(t, `UPDATE notification_deliveries SET next_attempt_at = now() WHERE id = $1`, id)
			continue
		}
		if status != "dead" || lastErr == nil || *lastErr != "respuesta http 503" {
			t.Errorf("tras %d intentos got %s (%v), want dead", attempt, status, lastErr)
		}
	}
	mustExec(t, `UPDATE notification_deliveries SET next_attempt_at = now() WHERE id = $1`, id)
	if jobs, _ := s.ClaimDeliveries(ctx, 50, 30*time.Second, time.Minute); len(jobs) != 0 {
		t.Error("una entrega dead no se reclama")
	}
}

func TestDeliveryOlderThan24HoursDiesOnFirstFailure(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ch := mkChan(t, s, 1, "hook")
	id := seedAlertDelivery(t, ch.ID, "h", "critical", "opened", 25*time.Hour)
	if _, err := s.ClaimDeliveries(ctx, 50, 30*time.Second, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkDeliveriesFailed(ctx, DeliveryFailure{IDs: []int64{id}, Delays: []time.Duration{time.Minute}, LastError: "x"}); err != nil {
		t.Fatal(err)
	}
	if status, _, _, _ := deliveryRow(t, id); status != "dead" {
		t.Errorf("got %s, want dead (mas de 24 h)", status)
	}
}

func TestSentReleasedAndSuppressedDeliveries(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ch := mkChan(t, s, 1, "hook")
	a := seedAlertDelivery(t, ch.ID, "h1", "critical", "opened", 0)
	b := seedAlertDelivery(t, ch.ID, "h2", "critical", "opened", 0)
	c := seedAlertDelivery(t, ch.ID, "h3", "critical", "opened", 0)
	if jobs, err := s.ClaimDeliveries(ctx, 50, 30*time.Second, time.Minute); err != nil || len(jobs) != 3 {
		t.Fatalf("reclamo got %d, %v", len(jobs), err)
	}

	// a y b salen en el mismo mensaje: cuentan como UN mensaje pa el limite por hora.
	if err := s.MarkDeliveriesSent(ctx, []int64{a, b}, 200); err != nil {
		t.Fatal(err)
	}
	if err := s.ReleaseDeliveries(ctx, []int64{c}, 90*time.Second, "limite"); err != nil {
		t.Fatal(err)
	}
	if status, _, _, _ := deliveryRow(t, a); status != "sent" {
		t.Errorf("a got %s", status)
	}
	status, attempts, lastErr, future := deliveryRow(t, c)
	if status != "pending" || attempts != 0 || !future || lastErr == nil {
		t.Errorf("c liberada got %s attempts=%d futuro=%v err=%v, want pending, 0 intentos gastados y en el futuro", status, attempts, future, lastErr)
	}
	sent, leaves, err := s.ChannelHourUsage(ctx, ch.ID)
	if err != nil || sent != 1 || leaves < 59*time.Minute || leaves > time.Hour {
		t.Errorf("ChannelHourUsage got %d, %v, %v; want 1 mensaje que sale de la ventana en ~1 h", sent, leaves, err)
	}
	if sent, leaves, _ := s.ChannelHourUsage(ctx, "00000000-0000-4000-8000-0000000000ff"); sent != 0 || leaves != 0 {
		t.Errorf("canal sin envios got %d, %v", sent, leaves)
	}
	// Un envio de hace mas de una hora ya no cuenta.
	mustExec(t, `UPDATE notification_deliveries SET sent_at = now() - interval '61 minutes' WHERE id IN ($1, $2)`, a, b)
	if sent, _, _ := s.ChannelHourUsage(ctx, ch.ID); sent != 0 {
		t.Errorf("fuera de la ventana got %d, want 0", sent)
	}

	mustExec(t, `UPDATE notification_deliveries SET status = 'sending', lease_until = now() + interval '1 minute' WHERE id = $1`, c)
	if err := s.SuppressDeliveries(ctx, []int64{c}, "canal deshabilitado"); err != nil {
		t.Fatal(err)
	}
	if status, _, _, _ := deliveryRow(t, c); status != "suppressed" {
		t.Errorf("c got %s, want suppressed", status)
	}
}

func TestDisablingAChannelSuppressesItsPendingDeliveries(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ch := mkChan(t, s, 1, "hook")
	other := mkChan(t, s, 2, "otro")
	mine := seedAlertDelivery(t, ch.ID, "h1", "critical", "opened", 0)
	theirs := seedAlertDelivery(t, other.ID, "h2", "critical", "opened", 0)

	if _, err := s.UpdateChannel(ctx, ch.ID, func(c *Channel) error { c.Enabled = false; return nil }); err != nil {
		t.Fatal(err)
	}
	if st, _, _, _ := deliveryRow(t, mine); st != "suppressed" {
		t.Errorf("entrega del canal deshabilitado got %s, want suppressed", st)
	}
	if st, _, _, _ := deliveryRow(t, theirs); st != "pending" {
		t.Errorf("entrega de otro canal got %s, want pending", st)
	}
}

func TestDeliveryIsUniquePerAlertChannelEventAndSeq(t *testing.T) {
	s := newTestStore(t)
	ch := mkChan(t, s, 1, "hook")
	id := seedAlertDelivery(t, ch.ID, "h1", "critical", "opened", 0)
	var alertID string
	if err := testPool.QueryRow(context.Background(), `SELECT alert_id FROM notification_deliveries WHERE id = $1`, id).Scan(&alertID); err != nil {
		t.Fatal(err)
	}
	_, err := testPool.Exec(context.Background(),
		`INSERT INTO notification_deliveries (alert_id, channel_id, event) VALUES ($1, $2, 'opened')`, alertID, ch.ID)
	if err == nil {
		t.Fatal("la misma alerta+canal+evento no debe encolarse dos veces")
	}
}
