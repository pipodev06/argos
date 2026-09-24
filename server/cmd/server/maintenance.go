package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"argos/server/internal/httpapi"
)

const (
	purgeInterval = time.Hour
	flushInterval = time.Minute
	purgeBatch    = 1000
	// Los enroll tokens vencidos se conservan 7 dias (rastro de quien intento usar uno vencido).
	enrollTokenGrace  = 7 * 24 * time.Hour
	finalFlushTimeout = 3 * time.Second
)

// purger es lo que el mantenimiento necesita del store.
type purger interface {
	PurgeSessions(ctx context.Context, now time.Time, idle time.Duration, batch int) (int, error)
	PurgeAudit(ctx context.Context, before time.Time, batch int) (int, error)
	PurgeEnrollTokens(ctx context.Context, before time.Time, batch int) (int, error)
	PurgeHostEvents(ctx context.Context, before time.Time, batch int) (int, error)
	PurgeResolvedAlerts(ctx context.Context, before time.Time, batch int) (int, error)
	PurgeMaintenanceWindows(ctx context.Context, before time.Time, batch int) (int, error)
	PurgeDeliveries(ctx context.Context, before time.Time, batch int) (int, error)
	PurgeCheckEvents(ctx context.Context, before time.Time, batch int) (int, error)
	PurgeStaleCheckState(ctx context.Context, now time.Time, batch int) (int, error)
}

// retentionPolicy es cuanto se conserva cada tabla que purga el mantenimiento.
type retentionPolicy struct {
	Audit         time.Duration
	HostEvents    time.Duration
	Alerts        time.Duration // alertas resueltas y ventanas de mantenimiento terminadas
	Notifications time.Duration // entregas terminadas
	CheckEvents   time.Duration // transiciones de chequeos de servicio
}

// runMaintenance corre hasta que ctx se cancele: vuelca cada minuto los fallos de
// auth de agentes agregados y cada hora purga sesiones vencidas, auditoria vieja, enroll
// tokens vencidos, eventos de host, alertas resueltas, ventanas de mantenimiento terminadas
// y entregas de notificaciones terminadas, en lotes. Al terminar hace un ultimo volcado de auditoria.
func runMaintenance(ctx context.Context, logger *slog.Logger, st purger, api *httpapi.API, keep retentionPolicy) {
	flush := time.NewTicker(flushInterval)
	defer flush.Stop()
	purge := time.NewTicker(purgeInterval)
	defer purge.Stop()

	purgeAll(ctx, logger, st, time.Now(), keep)
	for {
		select {
		case <-ctx.Done():
			final, cancel := context.WithTimeout(context.WithoutCancel(ctx), finalFlushTimeout)
			api.FlushAgentFailures(final)
			cancel()
			return
		case <-flush.C:
			api.FlushAgentFailures(ctx)
		case now := <-purge.C:
			purgeAll(ctx, logger, st, now, keep)
		}
	}
}

func purgeAll(ctx context.Context, logger *slog.Logger, st purger, now time.Time, keep retentionPolicy) {
	steps := []struct {
		name string
		run  func() (int, error)
	}{
		{"sessions", func() (int, error) { return st.PurgeSessions(ctx, now, httpapi.SessionIdleTimeout, purgeBatch) }},
		{"audit_log", func() (int, error) { return st.PurgeAudit(ctx, now.Add(-keep.Audit), purgeBatch) }},
		{"enroll_tokens", func() (int, error) { return st.PurgeEnrollTokens(ctx, now.Add(-enrollTokenGrace), purgeBatch) }},
		{"host_events", func() (int, error) { return st.PurgeHostEvents(ctx, now.Add(-keep.HostEvents), purgeBatch) }},
		{"alerts", func() (int, error) { return st.PurgeResolvedAlerts(ctx, now.Add(-keep.Alerts), purgeBatch) }},
		{"maintenance_windows", func() (int, error) { return st.PurgeMaintenanceWindows(ctx, now.Add(-keep.Alerts), purgeBatch) }},
		{"notification_deliveries", func() (int, error) { return st.PurgeDeliveries(ctx, now.Add(-keep.Notifications), purgeBatch) }},
		{"service_check_events", func() (int, error) { return st.PurgeCheckEvents(ctx, now.Add(-keep.CheckEvents), purgeBatch) }},
		{"service_check_state", func() (int, error) { return st.PurgeStaleCheckState(ctx, now, purgeBatch) }},
	}
	for _, step := range steps {
		total, err := purgeInBatches(ctx, step.run)
		if errors.Is(err, context.Canceled) {
			return // apagado en curso: no es un fallo de la purga
		}
		if err != nil {
			logger.Error("fallo la purga", "table", step.name, "err", err)
			continue
		}
		if total > 0 {
			logger.Info("purga completada", "table", step.name, "rows", total)
		}
	}
}

// purgeInBatches repite run mientras devuelva un lote completo (puede quedar mas por borrar).
func purgeInBatches(ctx context.Context, run func() (int, error)) (int, error) {
	total := 0
	for {
		n, err := run()
		total += n
		if err != nil {
			return total, err
		}
		if n < purgeBatch || ctx.Err() != nil {
			return total, nil
		}
	}
}
