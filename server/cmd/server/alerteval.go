package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"argos/server/internal/store"
)

// alertEvaluator es lo que el evaluador de alertas necesita del store.
type alertEvaluator interface {
	EvaluateAlerts(ctx context.Context) (store.EvalResult, error)
}

// runAlertEvaluator corre hasta que ctx se cancele: cada interval evalua las reglas de alerta (una transaccion
// bajo lock advisory: con varias instancias solo una trabaja). Durante los primeros grace tras el arranque no
// evalua nada: si el server (o la BD) estuvo caido, los agentes no pudieron reportar y el detector de hosts
// caidos tambien espera; juzgar antes abriria alertas falsas de "sin datos" o "host caido".
func runAlertEvaluator(ctx context.Context, logger *slog.Logger, st alertEvaluator, interval, grace time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	graceUntil := time.Now().Add(grace)

	logger.Info("evaluador de alertas activo", "interval", interval.String(), "grace", grace.String())
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if now.Before(graceUntil) {
				continue
			}
			evaluateAlerts(ctx, logger, st)
		}
	}
}

func evaluateAlerts(ctx context.Context, logger *slog.Logger, st alertEvaluator) {
	res, err := st.EvaluateAlerts(ctx)
	if errors.Is(err, context.Canceled) {
		return // apagado en curso: no es un fallo del evaluador
	}
	if err != nil {
		logger.Error("fallo la evaluacion de alertas", "err", err)
		return
	}
	if !res.Ran {
		logger.Debug("evaluacion de alertas en curso en otra instancia: ciclo saltado")
		return
	}
	if res.Opened+res.Resolved+res.Released+res.Reminders > 0 {
		logger.Info("alertas evaluadas", "opened", res.Opened, "resolved", res.Resolved,
			"released", res.Released, "reminders", res.Reminders)
	}
}
