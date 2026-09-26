package retention

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// dropPolicy decide que particiones de una tabla se pueden borrar.
type dropPolicy struct {
	table string
	// cutoff: se borra lo que termina (cota superior) a esa medianoche UTC o antes.
	cutoff time.Time
	// through, si no es nil, es la marca de agua del resumen por hora: ademas, una particion
	// solo se borra si termina a esa hora o antes (su contenido ya esta resumido).
	through *time.Time
	// blocked: la marca de agua se necesita pero no existe (o no se pudo leer): no se borra nada.
	blocked bool
	// unpacked son las particiones (por nombre) que aun no estan empaquetadas en metrics_chunks: no se borran.
	unpacked map[string]bool
}

// allowed indica si una particion que termina en upper (nil = sin cota) se puede borrar.
func (p dropPolicy) allowed(upper *time.Time) bool {
	if upper == nil || p.blocked || upper.After(p.cutoff) {
		return false
	}
	return p.through == nil || !upper.After(*p.through)
}

// allowedPart es allowed pa una particion concreta: ademas exige que no este pendiente de empaquetar.
func (p dropPolicy) allowedPart(part partition) bool {
	return p.allowed(part.upper) && !p.unpacked[part.name]
}

// dropExpired borra las particiones vencidas de metrics (con la guardia del resumen por hora y del empaquetado),
// de service_checks y de disk_samples (ambas sin resumenes, sin guardia). La marca de agua se lee
// de la BD en cada ciclo, nunca se asume.
func (m *Maintainer) dropExpired(ctx context.Context, conn *pgx.Conn, now time.Time) error {
	today := utcDay(now)
	var errs []error

	metrics := dropPolicy{table: tableMetrics, cutoff: today.AddDate(0, 0, -m.cfg.HotDays)}
	through, ok, err := readRollupThrough(ctx, conn)
	switch {
	case err != nil:
		metrics.blocked = true
		errs = append(errs, err)
	case !ok:
		metrics.blocked = true // nada resumido aun: no se puede borrar crudo sin perder historia
	default:
		metrics.through = &through
		// Empaqueta antes de borrar: lo que no quede empaquetado se conserva y se reintenta en el siguiente ciclo.
		unpacked, err := m.packExpired(ctx, conn, metrics)
		if err != nil {
			errs = append(errs, err)
			if unpacked == nil {
				metrics.blocked = true // ni siquiera se supo que falta empaquetar: no se borra nada
			}
		}
		metrics.unpacked = unpacked
	}
	checks := dropPolicy{table: tableServiceChecks, cutoff: today.AddDate(0, 0, -m.cfg.ServiceChecksDays)}
	disks := dropPolicy{table: tableDiskSamples, cutoff: today.AddDate(0, 0, -m.cfg.DiskDays)}

	for _, policy := range []dropPolicy{metrics, checks, disks} {
		if err := m.dropTable(ctx, conn, policy); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// dropTable borra las particiones permitidas por policy y las tablas huerfanas que dejo un
// DROP fallido tras un DETACH. Cada particion: DETACH CONCURRENTLY (fuera de transaccion, no
// bloquea lecturas ni el ingest) o FINALIZE si un DETACH anterior quedo pendiente, y luego DROP.
// Todo es idempotente: un ciclo interrumpido se completa en el siguiente.
func (m *Maintainer) dropTable(ctx context.Context, conn *pgx.Conn, policy dropPolicy) error {
	parts, err := m.listPartitions(ctx, conn, policy.table)
	if err != nil {
		return err
	}
	var errs []error
	// Las pendientes primero: Postgres no deja desacoplar otra mientras haya una pendiente.
	pending, rest := splitPending(parts)
	for _, p := range pending {
		if !policy.allowedPart(p) {
			m.log.Warn("particion con DETACH pendiente fuera de la ventana de borrado: se finaliza sin borrarla",
				"table", policy.table, "partition", p.name)
		}
		if err := m.finalizeDetach(ctx, conn, policy, p); err != nil {
			errs = append(errs, err)
		}
	}
	for _, p := range rest {
		if !policy.allowedPart(p) {
			continue
		}
		if err := m.detachAndDrop(ctx, conn, policy.table, p); err != nil {
			errs = append(errs, err)
			if isDetachFailure(err) {
				break // el DETACH quedo pendiente: las demas fallarian por lo mismo hasta que se finalice
			}
		}
	}
	if err := m.dropOrphans(ctx, conn, policy); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func splitPending(parts []partition) (pending, rest []partition) {
	for _, p := range parts {
		if p.detachPending {
			pending = append(pending, p)
		} else {
			rest = append(rest, p)
		}
	}
	return pending, rest
}

// detachError marca un fallo del DETACH (no del DROP).
type detachError struct{ err error }

func (e *detachError) Error() string { return e.err.Error() }
func (e *detachError) Unwrap() error { return e.err }

func isDetachFailure(err error) bool {
	var de *detachError
	return errors.As(err, &de)
}

func (m *Maintainer) finalizeDetach(ctx context.Context, conn *pgx.Conn, policy dropPolicy, p partition) error {
	parent := pgx.Identifier{policy.table}.Sanitize()
	child := pgx.Identifier{p.name}.Sanitize()
	stmt := fmt.Sprintf(`ALTER TABLE %s DETACH PARTITION %s FINALIZE`, parent, child)
	if _, err := conn.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("finalizando DETACH de %s: %w", p.name, err)
	}
	m.log.Info("DETACH pendiente finalizado", "table", policy.table, "partition", p.name)
	if !policy.allowedPart(p) {
		return nil
	}
	return m.dropDetached(ctx, conn, policy.table, p.name, p.upper)
}

func (m *Maintainer) detachAndDrop(ctx context.Context, conn *pgx.Conn, table string, p partition) error {
	parent := pgx.Identifier{table}.Sanitize()
	child := pgx.Identifier{p.name}.Sanitize()
	// CONCURRENTLY no puede ir dentro de una transaccion: Exec de una sola sentencia no abre una.
	stmt := fmt.Sprintf(`ALTER TABLE %s DETACH PARTITION %s CONCURRENTLY`, parent, child)
	if _, err := conn.Exec(ctx, stmt); err != nil {
		return &detachError{fmt.Errorf("desacoplando %s de %s: %w", p.name, table, err)}
	}
	return m.dropDetached(ctx, conn, table, p.name, p.upper)
}

func (m *Maintainer) dropDetached(ctx context.Context, conn *pgx.Conn, table, name string, upper *time.Time) error {
	stmt := fmt.Sprintf(`DROP TABLE %s`, pgx.Identifier{name}.Sanitize())
	if _, err := conn.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("borrando %s de %s: %w", name, table, err)
	}
	attrs := []any{"table", table, "partition", name}
	if upper != nil {
		attrs = append(attrs, "upper", upper.Format(time.RFC3339))
	}
	m.log.Info("particion eliminada", attrs...)
	return nil
}

// dropOrphans borra las tablas <table>_pYYYYMMDD (y <table>_legacy) que ya no son particiones:
// las deja un DETACH cuyo DROP fallo (p. ej. lock_timeout). Solo el propio mantenimiento las
// produce y solo tras pasar la guardia; las de nombre con fecha se re-verifican contra la
// ventana de borrado (la legacy no tiene fecha: su unica guardia fue la del DETACH).
func (m *Maintainer) dropOrphans(ctx context.Context, conn *pgx.Conn, policy dropPolicy) error {
	rows, err := conn.Query(ctx,
		`SELECT c.relname FROM pg_class c
		  WHERE c.relkind = 'r' AND NOT c.relispartition AND pg_table_is_visible(c.oid)
		    AND c.relname ~ $1`, "^"+regexp.QuoteMeta(policy.table)+"_(p[0-9]{8}|legacy)$")
	if err != nil {
		return fmt.Errorf("buscando tablas huerfanas de %s: %w", policy.table, err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("leyendo tablas huerfanas de %s: %w", policy.table, err)
	}

	var errs []error
	for _, name := range names {
		var upper *time.Time
		if suffix, dated := strings.CutPrefix(name, policy.table+"_p"); dated {
			day, err := time.Parse("20060102", suffix)
			if err != nil {
				continue
			}
			u := day.AddDate(0, 0, 1)
			upper = &u
			if !policy.allowed(upper) {
				m.log.Warn("tabla huerfana fuera de la ventana de borrado: se conserva", "table", policy.table, "orphan", name)
				continue
			}
		}
		if err := m.dropDetached(ctx, conn, policy.table, name, upper); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// purgeHourly borra en lotes los resumenes por hora anteriores a la retencion.
func (m *Maintainer) purgeHourly(ctx context.Context, conn *pgx.Conn, now time.Time) error {
	cutoff := utcDay(now).AddDate(0, 0, -m.cfg.HourlyDays)
	total := 0
	for {
		tag, err := conn.Exec(ctx,
			`DELETE FROM metrics_hourly
			  WHERE ctid = ANY (ARRAY(SELECT ctid FROM metrics_hourly WHERE hour < $1 LIMIT $2))`,
			cutoff, hourlyPurgeBatch)
		if err != nil {
			return fmt.Errorf("purgando metrics_hourly: %w", err)
		}
		n := int(tag.RowsAffected())
		total += n
		if n < hourlyPurgeBatch || ctx.Err() != nil {
			break
		}
	}
	if total > 0 {
		m.log.Info("purga completada", "table", "metrics_hourly", "rows", total)
	}
	return nil
}
