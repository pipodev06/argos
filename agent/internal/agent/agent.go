// Package agent contiene el loop principal: recolectar y reportar cada intervalo.
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"argos/agent/internal/quiet"
	"argos/agent/internal/sender"
)

// Submitter envia un reporte al servidor (lo implementa *sender.Client).
type Submitter interface {
	Send(ctx context.Context, r sender.Report) error
}

// Collector mide las metricas del host (lo implementa *collector.System).
type Collector interface {
	Collect(ctx context.Context) (sender.Metrics, error)
}

// DiskCollector mide el uso de los discos (lo implementa *collector.System).
type DiskCollector interface {
	Disks(ctx context.Context) ([]sender.DiskUsage, error)
}

// HostInfoCollector lee los datos del host (lo implementa *collector.System).
// No incluye agent_version: la pone el agente.
type HostInfoCollector interface {
	HostInfo(ctx context.Context) (sender.HostInfo, error)
}

// Options configura Run. Disks y HostInfo son opcionales: nil = ese dato no se manda.
type Options struct {
	Hostname     string
	Version      string
	Interval     time.Duration
	Collector    Collector
	Disks        DiskCollector
	HostInfo     HostInfoCollector
	DiskInterval time.Duration // cada cuanto se reportan los discos (tras el primer reporte)
	Now          func() time.Time
	Logger       *slog.Logger
}

// Run reporta inmediatamente y luego cada Interval hasta que ctx se cancele
// (devuelve nil) o el servidor rechace el token (devuelve un error que
// cumple errors.Is(err, sender.ErrTokenRejected): reintentar no sirve).
func Run(ctx context.Context, sub Submitter, o Options) error {
	sched := newSchedule(o.DiskInterval)
	track := quiet.New(o.Logger)
	report := func() error {
		m, err := o.Collector.Collect(ctx)
		if err != nil {
			// Sin medicion confiable no se manda nada (un cero falso engaña al dashboard).
			if ctx.Err() == nil {
				o.Logger.Error("no se pudieron medir las metricas, se omite este ciclo", "err", err)
			}
			return nil
		}
		r := sender.Report{
			Hostname:     o.Hostname,
			AgentVersion: o.Version,
			Timestamp:    o.Now().Unix(),
			Metrics:      m,
		}
		sched.attach(ctx, &r, o, track)
		err = sub.Send(ctx, r)
		switch {
		case err == nil, ctx.Err() != nil:
			return nil
		case errors.Is(err, sender.ErrTokenRejected):
			return fmt.Errorf("enviando metricas: %w", err)
		default:
			o.Logger.Error("no se pudieron enviar las metricas", "err", err)
			return nil
		}
	}

	if err := report(); err != nil { // primer envio inmediato
		return err
	}
	ticker := time.NewTicker(o.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := report(); err != nil {
				return err
			}
		}
	}
}
