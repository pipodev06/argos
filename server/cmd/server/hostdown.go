package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"argos/server/internal/store"
)

// hostDownMarker es lo que el detector necesita del store.
type hostDownMarker interface {
	MarkStaleHosts(ctx context.Context, globalDownAfter time.Duration) ([]store.StaleHost, error)
}

// runHostDownDetector corre hasta que ctx se cancele: cada interval marca los hosts up que llevan
// mas de su umbral sin reportar (el de su grupo o downAfter, el global): down si su grupo es
// always_on, offline si no. Durante los primeros downAfter tras el arranque no marca nada: si el
// server (o la BD) estuvo caido, los agentes no pudieron reportar y necesitan una ventana pa ponerse
// al dia antes de juzgarlos.
func runHostDownDetector(ctx context.Context, logger *slog.Logger, st hostDownMarker, downAfter, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	graceUntil := time.Now().Add(downAfter)

	logger.Info("detector de hosts caidos activo", "down_after", downAfter.String(), "interval", interval.String())
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if now.Before(graceUntil) {
				continue
			}
			markStaleHosts(ctx, logger, st, downAfter)
		}
	}
}

func markStaleHosts(ctx context.Context, logger *slog.Logger, st hostDownMarker, downAfter time.Duration) {
	stale, err := st.MarkStaleHosts(ctx, downAfter)
	if errors.Is(err, context.Canceled) {
		return // apagado en curso: no es un fallo del detector
	}
	if err != nil {
		logger.Error("fallo la deteccion de hosts sin reportar", "err", err)
		return
	}
	for _, h := range stale {
		if h.Status == "down" {
			logger.Warn("host caido", "host_id", h.ID, "hostname", h.Hostname, "global_down_after", downAfter.String())
			continue
		}
		// offline: equipo que se apaga o duerme (laptop), no requiere atencion.
		logger.Info("host offline", "host_id", h.ID, "hostname", h.Hostname, "global_down_after", downAfter.String())
	}
}
