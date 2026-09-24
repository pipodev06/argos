package retention

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	// rollupMarker es la fila de maintenance_state con la marca de agua del resumen: todas las
	// horas cerradas anteriores a ese instante ya estan resumidas en metrics_hourly.
	rollupMarker = "metrics_rollup_through"

	// rollupReprocessHours: cada ciclo vuelve a resumir las ultimas horas ya resumidas, porque el
	// buffer offline del agente entrega muestras tardias (hasta ~1 h) de horas ya cerradas.
	rollupReprocessHours = 3

	// rollupMaxCatchUp acota cuanto avanza la marca de agua por ciclo tras un apagado largo.
	rollupMaxCatchUp = 48 * time.Hour
)

// rollupSQL resume una ventana de horas ($1 inclusive, $2 exclusivo, alineadas a UTC) de metrics
// en metrics_hourly. Es idempotente: solo escribe filas cuyo resumen cambio. La tasa de red sale
// de los contadores acumulados con el mismo criterio que la API (reinicio de contador y hueco
// > 3x la mediana del intervalo no cuentan). Ver docs/decisiones.md.
//
//go:embed rollup.sql
var rollupSQL string

// readRollupThrough lee la marca de agua. ok=false si aun no hay (nunca se ha resumido).
func readRollupThrough(ctx context.Context, conn *pgx.Conn) (through time.Time, ok bool, err error) {
	err = conn.QueryRow(ctx, `SELECT value_ts FROM maintenance_state WHERE name = $1`, rollupMarker).Scan(&through)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("leyendo la marca de agua del resumen: %w", err)
	}
	return through.UTC(), true, nil
}

// rollup resume por hora las metricas crudas. Una transaccion por hora: el resumen de la hora y
// el avance de la marca de agua se confirman juntos, asi un corte a mitad de ciclo deja la marca
// coherente. Cada ciclo reprocesa las ultimas rollupReprocessHours horas ya resumidas (datos
// tardios) y, si la marca va atrasada, avanza como mucho rollupMaxCatchUp. Las horas sin datos no
// generan filas. Solo se resumen horas cerradas: la hora en curso aun no tiene resumen.
func (m *Maintainer) rollup(ctx context.Context, conn *pgx.Conn, now time.Time) error {
	target := now.UTC().Truncate(time.Hour) // primera hora aun abierta (exclusiva)

	through, ok, err := readRollupThrough(ctx, conn)
	if err != nil {
		return err
	}
	var start time.Time
	if ok {
		start = through.Truncate(time.Hour).Add(-rollupReprocessHours * time.Hour)
	} else {
		// Primer ciclo: arranca en la hora de la muestra mas antigua (si hay datos legacy, todo
		// su historial se resume en ciclos sucesivos de rollupMaxCatchUp).
		var oldest *time.Time
		if err := conn.QueryRow(ctx, `SELECT min(ts) FROM metrics`).Scan(&oldest); err != nil {
			return fmt.Errorf("buscando la muestra mas antigua: %w", err)
		}
		if oldest == nil {
			return setRollupThrough(ctx, conn, target) // sin datos: nada que resumir
		}
		through = oldest.UTC().Truncate(time.Hour)
		start = through
	}

	end := through.Add(rollupMaxCatchUp)
	if end.After(target) {
		end = target
	}
	hours := 0
	for h := start; h.Before(end); h = h.Add(time.Hour) {
		if err := rollupHour(ctx, conn, h); err != nil {
			return err
		}
		hours++
	}
	if hours > 0 {
		m.log.Debug("resumen por hora ejecutado", "from", start.Format(time.RFC3339), "to", end.Format(time.RFC3339))
	}
	return nil
}

// rollupHour resume la hora [h, h+1h) y avanza la marca de agua a h+1h, en una transaccion.
func rollupHour(ctx context.Context, conn *pgx.Conn, h time.Time) error {
	next := h.Add(time.Hour)
	err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, rollupSQL, h, next); err != nil {
			return fmt.Errorf("resumiendo: %w", err)
		}
		return advanceRollupThrough(ctx, tx, next)
	})
	if err != nil {
		return fmt.Errorf("resumen de la hora %s: %w", h.Format(time.RFC3339), err)
	}
	return nil
}

func setRollupThrough(ctx context.Context, conn *pgx.Conn, at time.Time) error {
	err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error { return advanceRollupThrough(ctx, tx, at) })
	if err != nil {
		return fmt.Errorf("fijando la marca de agua del resumen: %w", err)
	}
	return nil
}

// advanceRollupThrough sube la marca de agua a at; nunca la baja ni reescribe la fila si no avanza
// (la marca solo crece: reprocesar horas viejas no la mueve).
func advanceRollupThrough(ctx context.Context, tx pgx.Tx, at time.Time) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO maintenance_state (name, value_ts) VALUES ($1, $2)
		 ON CONFLICT (name) DO UPDATE SET value_ts = EXCLUDED.value_ts, updated_at = now()
		 WHERE maintenance_state.value_ts < EXCLUDED.value_ts`,
		rollupMarker, at)
	if err != nil {
		return fmt.Errorf("avanzando la marca de agua: %w", err)
	}
	return nil
}
