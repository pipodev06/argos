package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// InventoryUpdates es el modulo de parches del SO: comparte endpoint, rev y latido con el inventario, pero su foto
// es un objeto con columnas tipadas (updates_current), no una lista.
const InventoryUpdates = "updates"

// maxUpdatesHistoryPerDay es el tope de filas de historial por host y dia (UTC): un agente que alterne valores sin
// parar no llena la tabla. Los envios que no caben siguen actualizando la foto actual.
const maxUpdatesHistoryPerDay = 100

// UpdatesSnapshot es el envio completo del modulo updates, ya validado. Cada campo con puntero es tri-estado:
// nil = desconocido (no es 0 ni false).
type UpdatesSnapshot struct {
	Rev                 string
	PendingTotal        *int
	PendingSecurity     *int
	RebootRequired      *bool
	LastInstalled       *time.Time // solo fecha (UTC)
	Source              string
	CacheAgeS           *int64
	Advisories          []string // ids de aviso, sin repetir y ordenados
	AdvisoriesTruncated bool
	CollectedAt         time.Time
}

// UpdatesCurrent es la ultima foto guardada, con las marcas derivadas en el servidor.
type UpdatesCurrent struct {
	UpdatesSnapshot
	RebootSince          *time.Time // primera vez que se vio reinicio pendiente; nil si no lo hay o se desconoce
	SecurityPendingSince *time.Time // primera vez con pendientes de seguridad (> 0); nil si no los hay o se desconoce
	ReceivedAt           time.Time  // ultimo envio o latido
}

// sinceMark fija la marca cuando la condicion pasa a cumplirse (o la conserva si ya se cumplia) y la limpia cuando
// deja de cumplirse o pasa a desconocida.
func sinceMark(holds bool, prev *time.Time, now time.Time) *time.Time {
	switch {
	case !holds:
		return nil
	case prev != nil:
		return prev
	default:
		return &now
	}
}

func equalPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

const updatesColumns = `rev, pending_total, pending_security, reboot_required, reboot_since, security_pending_since,
	last_installed, source, cache_age_s, advisories, advisories_truncated, collected_at, received_at`

func scanUpdates(row pgx.Row) (UpdatesCurrent, error) {
	var u UpdatesCurrent
	err := row.Scan(&u.Rev, &u.PendingTotal, &u.PendingSecurity, &u.RebootRequired, &u.RebootSince, &u.SecurityPendingSince,
		&u.LastInstalled, &u.Source, &u.CacheAgeS, &u.Advisories, &u.AdvisoriesTruncated, &u.CollectedAt, &u.ReceivedAt)
	return u, err
}

// IngestUpdates guarda la foto de parches del host. La primera es la linea base; despues calcula reboot_since y
// security_pending_since contra la fila anterior (bloqueada) y agrega una fila de historial solo si cambiaron los
// contadores o el reinicio. Reenviar el mismo rev es idempotente (no agrega historial).
func (s *Store) IngestUpdates(ctx context.Context, hostID string, in UpdatesSnapshot, now time.Time) error {
	advisories := in.Advisories
	if advisories == nil {
		advisories = []string{}
	}
	rebootNow := in.RebootRequired != nil && *in.RebootRequired
	secNow := in.PendingSecurity != nil && *in.PendingSecurity > 0
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Inserta la linea base sin carrera entre dos primeros envios; si ya existia, la lee bloqueada.
		tag, err := tx.Exec(ctx,
			`INSERT INTO updates_current (host_id, `+updatesColumns+`)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) ON CONFLICT (host_id) DO NOTHING`,
			hostID, in.Rev, in.PendingTotal, in.PendingSecurity, in.RebootRequired,
			sinceMark(rebootNow, nil, now), sinceMark(secNow, nil, now),
			in.LastInstalled, in.Source, in.CacheAgeS, advisories, in.AdvisoriesTruncated, in.CollectedAt, now)
		if err != nil {
			return fmt.Errorf("insertando la foto de parches: %w", err)
		}
		if tag.RowsAffected() == 1 {
			return insertUpdatesHistory(ctx, tx, hostID, in, now)
		}
		prev, err := scanUpdates(tx.QueryRow(ctx, `SELECT `+updatesColumns+` FROM updates_current WHERE host_id = $1 FOR UPDATE`, hostID))
		if err != nil {
			return fmt.Errorf("leyendo la foto de parches actual: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE updates_current SET rev = $2, pending_total = $3, pending_security = $4, reboot_required = $5,
			        reboot_since = $6, security_pending_since = $7, last_installed = $8, source = $9, cache_age_s = $10,
			        advisories = $11, advisories_truncated = $12, collected_at = $13, received_at = $14
			  WHERE host_id = $1`,
			hostID, in.Rev, in.PendingTotal, in.PendingSecurity, in.RebootRequired,
			sinceMark(rebootNow, prev.RebootSince, now), sinceMark(secNow, prev.SecurityPendingSince, now),
			in.LastInstalled, in.Source, in.CacheAgeS, advisories, in.AdvisoriesTruncated, in.CollectedAt, now); err != nil {
			return fmt.Errorf("actualizando la foto de parches: %w", err)
		}
		if equalPtr(prev.PendingTotal, in.PendingTotal) && equalPtr(prev.PendingSecurity, in.PendingSecurity) &&
			equalPtr(prev.RebootRequired, in.RebootRequired) {
			return nil
		}
		return insertUpdatesHistory(ctx, tx, hostID, in, now)
	})
	if err != nil {
		return fmt.Errorf("guardando parches: %w", err)
	}
	return nil
}

// insertUpdatesHistory agrega un punto de historial respetando el tope diario por host.
func insertUpdatesHistory(ctx context.Context, tx pgx.Tx, hostID string, in UpdatesSnapshot, now time.Time) error {
	var today int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM updates_history WHERE host_id = $1 AND at >= $2`,
		hostID, now.UTC().Truncate(24*time.Hour)).Scan(&today); err != nil {
		return fmt.Errorf("contando historial de parches del dia: %w", err)
	}
	if today >= maxUpdatesHistoryPerDay {
		return nil
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO updates_history (host_id, at, pending_total, pending_security, reboot_required) VALUES ($1, $2, $3, $4, $5)`,
		hostID, now, in.PendingTotal, in.PendingSecurity, in.RebootRequired); err != nil {
		return fmt.Errorf("insertando historial de parches: %w", err)
	}
	return nil
}

// updatesHeartbeat confirma que la foto de parches no cambio. ErrNeedFull si el rev no coincide o no hay foto.
func (s *Store) updatesHeartbeat(ctx context.Context, hostID, rev string, now time.Time) error {
	tag, err := s.pool.Exec(ctx, `UPDATE updates_current SET received_at = $3 WHERE host_id = $1 AND rev = $2`, hostID, rev, now)
	if err != nil {
		return fmt.Errorf("registrando latido de parches: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNeedFull
	}
	return nil
}

// PurgeUpdatesHistory borra historial de parches anterior a before.
func (s *Store) PurgeUpdatesHistory(ctx context.Context, before time.Time, batch int) (int, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM updates_history WHERE id IN (SELECT id FROM updates_history WHERE at < $1 ORDER BY at LIMIT $2)`,
		before, batch)
	if err != nil {
		return 0, fmt.Errorf("purgando historial de parches: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// HostUpdates devuelve la ultima foto de parches del host. ErrNotFound si el host no existe o esta fuera del
// ambito; found=false si el host nunca reporto el modulo.
func (s *Store) HostUpdates(ctx context.Context, scope Scope, hostID string) (UpdatesCurrent, bool, error) {
	if err := s.requireHost(ctx, scope, hostID); err != nil {
		return UpdatesCurrent{}, false, err
	}
	u, err := scanUpdates(s.pool.QueryRow(ctx, `SELECT `+updatesColumns+` FROM updates_current WHERE host_id = $1`, hostID))
	if errors.Is(err, pgx.ErrNoRows) {
		return UpdatesCurrent{}, false, nil
	}
	if err != nil {
		return UpdatesCurrent{}, false, fmt.Errorf("leyendo parches del host: %w", err)
	}
	return u, true, nil
}

// UpdatesPoint es un punto del historial de parches.
type UpdatesPoint struct {
	At              time.Time
	PendingTotal    *int
	PendingSecurity *int
	RebootRequired  *bool
}

// HostUpdatesHistory devuelve hasta limit puntos del historial, los mas recientes primero. ErrNotFound si el host
// no existe o esta fuera del ambito.
func (s *Store) HostUpdatesHistory(ctx context.Context, scope Scope, hostID string, limit int) ([]UpdatesPoint, error) {
	if err := s.requireHost(ctx, scope, hostID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT at, pending_total, pending_security, reboot_required FROM updates_history
		  WHERE host_id = $1 ORDER BY at DESC, id DESC LIMIT $2`, hostID, limit)
	if err != nil {
		return nil, fmt.Errorf("listando historial de parches: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (UpdatesPoint, error) {
		var p UpdatesPoint
		err := row.Scan(&p.At, &p.PendingTotal, &p.PendingSecurity, &p.RebootRequired)
		return p, err
	})
	if err != nil {
		return nil, fmt.Errorf("leyendo historial de parches: %w", err)
	}
	return out, nil
}

// OSEOL es el fin de vida resuelto del SO de un host.
type OSEOL struct {
	Family, Cycle, Label string
	EOLDate              time.Time // solo fecha (UTC)
	LTS                  bool
	Estimated            bool // la fecha es una estimacion, no publicada por el proveedor
}

// HostOSEOL resuelve el fin de vida del SO del host con la clave hosts.eol_family/eol_cycle (derivada de hosts.os y host_info por trigger). ErrNotFound si el
// host no existe o esta fuera del ambito; found=false si el SO no se reconoce o no esta en la tabla (no es un error:
// "sin dato de fin de vida").
func (s *Store) HostOSEOL(ctx context.Context, scope Scope, hostID string) (OSEOL, bool, error) {
	if err := s.requireHost(ctx, scope, hostID); err != nil {
		return OSEOL{}, false, err
	}
	var e OSEOL
	var source string
	err := s.pool.QueryRow(ctx,
		`SELECT e.family, e.cycle, e.label, e.eol_date, e.lts, e.source
		   FROM hosts h
		   JOIN os_eol e ON e.family = h.eol_family AND e.cycle = h.eol_cycle
		  WHERE h.id = $1`, hostID).Scan(&e.Family, &e.Cycle, &e.Label, &e.EOLDate, &e.LTS, &source)
	if errors.Is(err, pgx.ErrNoRows) {
		return OSEOL{}, false, nil
	}
	if err != nil {
		return OSEOL{}, false, fmt.Errorf("resolviendo fin de vida del SO: %w", err)
	}
	e.Estimated = source == "estimate"
	return e, true, nil
}
