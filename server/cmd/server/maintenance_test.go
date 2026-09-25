package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

type fakePurger struct {
	sessions, audit, tokens, events, alerts, windows, deliveries, checkEvents, checkState, inventory []int // filas que devuelve cada llamada, en orden
	calls                                                                                            map[string]int
	auditBefore, tokensBefore                                                                        time.Time
	before                                                                                           map[string]time.Time // corte usado por cada tabla nueva
	err                                                                                              error
}

func (f *fakePurger) next(name string, queue *[]int) (int, error) {
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[name]++
	if f.err != nil {
		return 0, f.err
	}
	if len(*queue) == 0 {
		return 0, nil
	}
	n := (*queue)[0]
	*queue = (*queue)[1:]
	return n, nil
}

func (f *fakePurger) PurgeSessions(_ context.Context, _ time.Time, _ time.Duration, batch int) (int, error) {
	if batch != purgeBatch {
		return 0, errors.New("lote inesperado")
	}
	return f.next("sessions", &f.sessions)
}

func (f *fakePurger) PurgeAudit(_ context.Context, before time.Time, _ int) (int, error) {
	f.auditBefore = before
	return f.next("audit", &f.audit)
}

func (f *fakePurger) PurgeEnrollTokens(_ context.Context, before time.Time, _ int) (int, error) {
	f.tokensBefore = before
	return f.next("tokens", &f.tokens)
}

func (f *fakePurger) purge(name string, queue *[]int, before time.Time) (int, error) {
	if f.before == nil {
		f.before = map[string]time.Time{}
	}
	f.before[name] = before
	return f.next(name, queue)
}

func (f *fakePurger) PurgeHostEvents(_ context.Context, before time.Time, _ int) (int, error) {
	return f.purge("events", &f.events, before)
}

func (f *fakePurger) PurgeResolvedAlerts(_ context.Context, before time.Time, _ int) (int, error) {
	return f.purge("alerts", &f.alerts, before)
}

func (f *fakePurger) PurgeMaintenanceWindows(_ context.Context, before time.Time, _ int) (int, error) {
	return f.purge("windows", &f.windows, before)
}

func (f *fakePurger) PurgeCheckEvents(_ context.Context, before time.Time, _ int) (int, error) {
	return f.purge("check_events", &f.checkEvents, before)
}

func (f *fakePurger) PurgeInventoryChanges(_ context.Context, before time.Time, _ int) (int, error) {
	return f.purge("inventory", &f.inventory, before)
}

func (f *fakePurger) PurgeStaleCheckState(_ context.Context, now time.Time, _ int) (int, error) {
	return f.purge("check_state", &f.checkState, now)
}

func (f *fakePurger) PurgeDeliveries(_ context.Context, before time.Time, _ int) (int, error) {
	return f.purge("deliveries", &f.deliveries, before)
}

func TestPurgeAllRepeatsWhileBatchesAreFull(t *testing.T) {
	t.Parallel()
	f := &fakePurger{
		sessions: []int{purgeBatch, purgeBatch, 10}, // 3 llamadas: dos lotes llenos y uno parcial
		audit:    []int{5},                          // 1 llamada
	}
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	keep := retentionPolicy{Audit: 180 * 24 * time.Hour, HostEvents: 180 * 24 * time.Hour, Alerts: 365 * 24 * time.Hour,
		Notifications: 30 * 24 * time.Hour, CheckEvents: 180 * 24 * time.Hour, Inventory: 90 * 24 * time.Hour}
	purgeAll(context.Background(), slog.New(slog.NewJSONHandler(io.Discard, nil)), f, now, keep)

	if f.calls["sessions"] != 3 || f.calls["audit"] != 1 || f.calls["tokens"] != 1 {
		t.Errorf("llamadas got %v, want sessions=3 audit=1 tokens=1", f.calls)
	}
	for name, want := range map[string]time.Duration{
		"events": keep.HostEvents, "alerts": keep.Alerts, "windows": keep.Alerts, "deliveries": keep.Notifications,
		"check_events": keep.CheckEvents, "inventory": keep.Inventory, "check_state": 0,
	} {
		if f.calls[name] != 1 || !f.before[name].Equal(now.Add(-want)) {
			t.Errorf("%s: llamadas %d, corte %v; want 1 llamada con corte %v", name, f.calls[name], f.before[name], now.Add(-want))
		}
	}
	if want := now.Add(-180 * 24 * time.Hour); !f.auditBefore.Equal(want) {
		t.Errorf("corte de audit got %v, want %v (retencion configurable)", f.auditBefore, want)
	}
	if want := now.Add(-7 * 24 * time.Hour); !f.tokensBefore.Equal(want) {
		t.Errorf("corte de enroll tokens got %v, want %v (vencidos hace > 7 dias)", f.tokensBefore, want)
	}
}

func TestPurgeAllContinuesAfterAnError(t *testing.T) {
	t.Parallel()
	f := &fakePurger{err: errors.New("db caida")}
	purgeAll(context.Background(), slog.New(slog.NewJSONHandler(io.Discard, nil)), f, time.Now(), retentionPolicy{Audit: 30 * 24 * time.Hour})
	if f.calls["sessions"] != 1 || f.calls["audit"] != 1 || f.calls["tokens"] != 1 || f.calls["deliveries"] != 1 {
		t.Errorf("un fallo en una tabla no debe frenar las demas: %v", f.calls)
	}
}

func TestPurgeInBatchesStopsWhenContextIsCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	total, err := purgeInBatches(ctx, func() (int, error) {
		calls++
		cancel() // el apagado llega en medio de la purga
		return purgeBatch, nil
	})
	if err != nil || calls != 1 || total != purgeBatch {
		t.Errorf("got (total=%d, err=%v, calls=%d), want una sola vuelta", total, err, calls)
	}
}

// Regresion: con una retencion de alertas corta, las ventanas de mantenimiento se conservan lo que necesita el
// informe de SLA de 90 d; con una larga manda la retencion de alertas.
func TestPurgeAllKeepsMaintenanceWindowsForSLAReports(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct{ alerts, want time.Duration }{
		"alertas cortas":  {30 * 24 * time.Hour, minMaintenanceKeep},
		"alertas largas":  {365 * 24 * time.Hour, 365 * 24 * time.Hour},
		"justo el minimo": {minMaintenanceKeep, minMaintenanceKeep},
	} {
		f := &fakePurger{}
		purgeAll(context.Background(), slog.New(slog.NewJSONHandler(io.Discard, nil)), f, now, retentionPolicy{Alerts: tc.alerts})
		if got, want := f.before["windows"], now.Add(-tc.want); !got.Equal(want) {
			t.Errorf("%s: corte de ventanas got %v, want %v", name, got, want)
		}
		if minMaintenanceKeep < 90*24*time.Hour {
			t.Fatalf("minMaintenanceKeep %v no cubre el informe de 90 d", minMaintenanceKeep)
		}
	}
}
