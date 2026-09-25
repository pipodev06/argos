package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// MaintenanceWindow es una ventana de mantenimiento: mientras esta activa, las alertas de los hosts de su
// alcance se abren visibles pero sin notificar.
type MaintenanceWindow struct {
	ID       string
	Name     string
	StartsAt time.Time
	EndsAt   time.Time
	Scope    AlertScope
	// GroupName es el nombre del grupo del alcance (solo lectura).
	GroupName *string
	// HostName es el nombre del host o dispositivo del alcance (solo lectura).
	HostName      *string
	CreatedByName *string
	CreatedAt     time.Time
	// Status (solo lectura, segun el reloj de la BD): scheduled, active o ended.
	Status string
}

// maintenanceListWindow es cuanto tiempo atras se listan las ventanas ya terminadas.
const maintenanceListWindow = "30 days"

const maintenanceSelect = `
SELECT m.id, m.name, m.starts_at, m.ends_at, m.scope_type, m.scope_group_id,
       g.name, m.scope_host_id, sh.hostname, m.created_by_name, m.created_at,
       CASE WHEN m.ends_at <= now() THEN 'ended' WHEN m.starts_at > now() THEN 'scheduled' ELSE 'active' END
  FROM maintenance_windows m LEFT JOIN host_groups g ON g.id = m.scope_group_id
  LEFT JOIN hosts sh ON sh.id = m.scope_host_id`

func scanWindow(row pgx.Row) (MaintenanceWindow, error) {
	var w MaintenanceWindow
	err := row.Scan(&w.ID, &w.Name, &w.StartsAt, &w.EndsAt, &w.Scope.Type, &w.Scope.GroupID,
		&w.GroupName, &w.Scope.HostID, &w.HostName, &w.CreatedByName, &w.CreatedAt, &w.Status)
	return w, err
}

func windowByID(ctx context.Context, q rowQuerier, id, lock string) (MaintenanceWindow, error) {
	w, err := scanWindow(q.QueryRow(ctx, maintenanceSelect+` WHERE m.id = $1 `+lock, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return MaintenanceWindow{}, ErrNotFound
	}
	if err != nil {
		return MaintenanceWindow{}, fmt.Errorf("leyendo ventana de mantenimiento: %w", err)
	}
	return w, nil
}

// ListMaintenance devuelve las ventanas activas, programadas y las terminadas en los ultimos 30 dias
// (las mas recientes primero). Un ambito restringido solo ve las de alcance `all`, las de sus grupos y las de un host o dispositivo de sus grupos.
func (s *Store) ListMaintenance(ctx context.Context, scope Scope) ([]MaintenanceWindow, error) {
	args := argList{MaxActiveMaintenance * 4}
	rows, err := s.pool.Query(ctx,
		maintenanceSelect+` WHERE m.ends_at > now() - interval '`+maintenanceListWindow+`' AND `+
			scope.configCondHost("m.scope_type", "m.scope_group_id", "m.scope_host_id", &args)+` ORDER BY m.starts_at DESC, m.id LIMIT $1`,
		[]any(args)...)
	if err != nil {
		return nil, fmt.Errorf("listando ventanas de mantenimiento: %w", err)
	}
	ws, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (MaintenanceWindow, error) { return scanWindow(row) })
	if err != nil {
		return nil, fmt.Errorf("leyendo ventanas de mantenimiento: %w", err)
	}
	return ws, nil
}

// MaintenanceByID devuelve una ventana. ErrNotFound si no existe o queda fuera del ambito.
func (s *Store) MaintenanceByID(ctx context.Context, scope Scope, id string) (MaintenanceWindow, error) {
	w, err := windowByID(ctx, s.pool, id, "")
	if err != nil {
		return MaintenanceWindow{}, err
	}
	if err := s.requireScopeVisible(ctx, scope, w.Scope); err != nil {
		return MaintenanceWindow{}, err
	}
	return w, nil
}

// CreateMaintenance crea una ventana (ya validada). ErrTooManyWindows si ya hay MaxActiveMaintenance activas o
// programadas (el lock advisory hace exacto el tope), ErrGroupNotFound si el grupo del alcance no existe.
func (s *Store) CreateMaintenance(ctx context.Context, w MaintenanceWindow, userID, username string) (MaintenanceWindow, error) {
	var out MaintenanceWindow
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('argos.maintenance.cap'))`); err != nil {
			return fmt.Errorf("lock de mantenimiento: %w", err)
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM maintenance_windows WHERE ends_at > now()`).Scan(&n); err != nil {
			return fmt.Errorf("contando ventanas: %w", err)
		}
		if n >= MaxActiveMaintenance {
			return ErrTooManyWindows
		}
		if err := requireScopeHost(ctx, tx, w.Scope); err != nil {
			return err
		}
		var id string
		err := tx.QueryRow(ctx,
			`INSERT INTO maintenance_windows (name, starts_at, ends_at, scope_type, scope_group_id,
			                                  scope_host_id, created_by, created_by_name)
			 VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, '')::uuid, NULLIF($8, ''))
			 RETURNING id`,
			w.Name, w.StartsAt, w.EndsAt, w.Scope.Type, w.Scope.GroupID, w.Scope.HostID,
			userID, username).Scan(&id)
		if isFKViolation(err, windowGroupFKey) {
			return ErrGroupNotFound
		}
		if isFKViolation(err, windowHostFKey) {
			return scopeHostGone(w.Scope)
		}
		if err != nil {
			return fmt.Errorf("insertando ventana de mantenimiento: %w", err)
		}
		out, err = windowByID(ctx, tx, id, "")
		return err
	})
	if err != nil {
		return MaintenanceWindow{}, err
	}
	return out, nil
}

// UpdateMaintenance bloquea la ventana, deja que mutate la modifique (su error se devuelve tal cual) y la
// guarda. ErrNotFound o ErrGroupNotFound segun el caso.
func (s *Store) UpdateMaintenance(ctx context.Context, id string, mutate func(*MaintenanceWindow) error) (MaintenanceWindow, error) {
	var out MaintenanceWindow
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		w, err := windowByID(ctx, tx, id, "FOR UPDATE OF m")
		if err != nil {
			return err
		}
		if err := mutate(&w); err != nil {
			return err
		}
		if err := requireScopeHost(ctx, tx, w.Scope); err != nil {
			return err
		}
		_, err = tx.Exec(ctx,
			`UPDATE maintenance_windows SET name = $2, starts_at = $3, ends_at = $4, scope_type = $5,
			        scope_group_id = $6, scope_host_id = $7
			  WHERE id = $1`,
			id, w.Name, w.StartsAt, w.EndsAt, w.Scope.Type, w.Scope.GroupID, w.Scope.HostID)
		if isFKViolation(err, windowGroupFKey) {
			return ErrGroupNotFound
		}
		if isFKViolation(err, windowHostFKey) {
			return scopeHostGone(w.Scope)
		}
		if err != nil {
			return fmt.Errorf("actualizando ventana de mantenimiento: %w", err)
		}
		out, err = windowByID(ctx, tx, id, "")
		return err
	})
	if err != nil {
		return MaintenanceWindow{}, err
	}
	return out, nil
}

// DeleteMaintenance borra la ventana (termina la supresion de inmediato) y devuelve su nombre.
func (s *Store) DeleteMaintenance(ctx context.Context, id string) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, `DELETE FROM maintenance_windows WHERE id = $1 RETURNING name`, id).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("borrando ventana de mantenimiento: %w", err)
	}
	return name, nil
}
