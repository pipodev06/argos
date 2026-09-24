package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	// ErrRuleExists: ya hay una regla de alerta con ese nombre (sin distinguir mayusculas).
	ErrRuleExists = errors.New("ya existe una regla con ese nombre")
	// ErrTooManyRules: ya hay MaxAlertRules reglas.
	ErrTooManyRules = errors.New("demasiadas reglas de alerta")
	// ErrTooManyWindows: ya hay MaxActiveMaintenance ventanas de mantenimiento activas o programadas.
	ErrTooManyWindows = errors.New("demasiadas ventanas de mantenimiento")
)

// Topes de cardinalidad.
const (
	MaxAlertRules        = 200
	MaxActiveMaintenance = 50
)

// Tipos de alcance de una regla o de una ventana de mantenimiento.
const (
	ScopeAll   = "all"
	ScopeGroup = "group"
	ScopeTag   = "tag"
)

// AlertScope es el alcance: todos los hosts, los de un grupo o los que tienen una etiqueta (sin distinguir mayusculas).
type AlertScope struct {
	Type    string
	GroupID *string
	Tag     *string
}

// AlertRule es una regla de alerta.
type AlertRule struct {
	ID              string
	Name            string
	Kind            string
	Metric          *string
	Op              *string
	Threshold       *float64
	ClearThreshold  *float64
	DurationSeconds int
	ResolveSeconds  int
	IncludeOffline  bool
	Scope           AlertScope
	// GroupName es el nombre del grupo del alcance (solo lectura).
	GroupName *string
	Severity  string
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
	// ActiveAlerts cuenta las alertas no resueltas de la regla (solo lectura).
	ActiveAlerts int
	// ChannelIDs son los canales de notificacion asignados (se cambian con SetRuleChannels).
	ChannelIDs []string
}

const alertRuleSelect = `
SELECT r.id, r.name, r.kind, r.metric, r.op, r.threshold, r.clear_threshold, r.duration_seconds, r.resolve_seconds,
       r.include_offline, r.scope_type, r.scope_group_id, r.scope_tag, g.name,
       r.severity, r.enabled, r.created_at, r.updated_at,
       (SELECT count(*)::int FROM alerts a WHERE a.rule_id = r.id AND a.state <> 'resolved'),
       COALESCE((SELECT array_agg(rc.channel_id::text ORDER BY rc.channel_id) FROM rule_channels rc WHERE rc.rule_id = r.id), '{}')
  FROM alert_rules r LEFT JOIN host_groups g ON g.id = r.scope_group_id`

func scanAlertRule(row pgx.Row) (AlertRule, error) {
	var r AlertRule
	err := row.Scan(&r.ID, &r.Name, &r.Kind, &r.Metric, &r.Op, &r.Threshold, &r.ClearThreshold, &r.DurationSeconds,
		&r.ResolveSeconds, &r.IncludeOffline, &r.Scope.Type, &r.Scope.GroupID, &r.Scope.Tag,
		&r.GroupName, &r.Severity, &r.Enabled, &r.CreatedAt, &r.UpdatedAt, &r.ActiveAlerts, &r.ChannelIDs)
	return r, err
}

// Nombres de constraint que se traducen a errores de dominio.
const (
	ruleNameKey     = "alert_rules_name_lower_key"
	ruleGroupFKey   = "alert_rules_scope_group_id_fkey"
	windowGroupFKey = "maintenance_windows_scope_group_id_fkey"
)

// ListAlertRules devuelve todas las reglas por nombre (hay un tope de MaxAlertRules).
func (s *Store) ListAlertRules(ctx context.Context) ([]AlertRule, error) {
	rows, err := s.pool.Query(ctx, alertRuleSelect+` ORDER BY lower(r.name), r.id LIMIT $1`, MaxAlertRules)
	if err != nil {
		return nil, fmt.Errorf("listando reglas de alerta: %w", err)
	}
	rules, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (AlertRule, error) { return scanAlertRule(row) })
	if err != nil {
		return nil, fmt.Errorf("leyendo reglas de alerta: %w", err)
	}
	return rules, nil
}

// AlertRuleByID devuelve una regla. ErrNotFound si no existe.
func (s *Store) AlertRuleByID(ctx context.Context, id string) (AlertRule, error) {
	return alertRuleByID(ctx, s.pool, id, "")
}

type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// alertRuleByID lee una regla; lock = "FOR UPDATE OF r" pa bloquear la fila dentro de una transaccion.
func alertRuleByID(ctx context.Context, q rowQuerier, id, lock string) (AlertRule, error) {
	r, err := scanAlertRule(q.QueryRow(ctx, alertRuleSelect+` WHERE r.id = $1 `+lock, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return AlertRule{}, ErrNotFound
	}
	if err != nil {
		return AlertRule{}, fmt.Errorf("leyendo regla de alerta: %w", err)
	}
	return r, nil
}

// CreateAlertRule crea una regla (ya validada por quien llama; los CHECK de la BD son la ultima defensa).
// ErrTooManyRules si ya hay MaxAlertRules, ErrRuleExists si el nombre choca, ErrGroupNotFound si el grupo
// del alcance no existe.
func (s *Store) CreateAlertRule(ctx context.Context, r AlertRule, createdBy string) (AlertRule, error) {
	var out AlertRule
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('argos.alert_rules.cap'))`); err != nil {
			return fmt.Errorf("lock de reglas: %w", err)
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM alert_rules`).Scan(&n); err != nil {
			return fmt.Errorf("contando reglas: %w", err)
		}
		if n >= MaxAlertRules {
			return ErrTooManyRules
		}
		var id string
		err := tx.QueryRow(ctx,
			`INSERT INTO alert_rules (name, kind, metric, op, threshold, clear_threshold, duration_seconds, resolve_seconds,
			                          include_offline, scope_type, scope_group_id, scope_tag,
			                          severity, enabled, created_by)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, NULLIF($15, '')::uuid)
			 RETURNING id`,
			r.Name, r.Kind, r.Metric, r.Op, r.Threshold, r.ClearThreshold, r.DurationSeconds, r.ResolveSeconds,
			r.IncludeOffline, r.Scope.Type, r.Scope.GroupID, r.Scope.Tag,
			r.Severity, r.Enabled, createdBy).Scan(&id)
		if err != nil {
			return mapRuleErr(err, "insertando regla de alerta")
		}
		out, err = alertRuleByID(ctx, tx, id, "")
		return err
	})
	if err != nil {
		return AlertRule{}, err
	}
	return out, nil
}

func mapRuleErr(err error, what string) error {
	switch {
	case isUniqueViolation(err, ruleNameKey):
		return ErrRuleExists
	case isFKViolation(err, ruleGroupFKey):
		return ErrGroupNotFound
	}
	return fmt.Errorf("%s: %w", what, err)
}

// UpdateAlertRule bloquea la regla, deja que mutate la modifique (validando; su error se devuelve tal cual) y
// guarda los campos editables (no kind ni metric). ErrNotFound, ErrRuleExists o ErrGroupNotFound segun el caso.
func (s *Store) UpdateAlertRule(ctx context.Context, id string, mutate func(*AlertRule) error) (AlertRule, error) {
	var out AlertRule
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		r, err := alertRuleByID(ctx, tx, id, "FOR UPDATE OF r")
		if err != nil {
			return err
		}
		if err := mutate(&r); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE alert_rules SET name = $2, op = $3, threshold = $4, clear_threshold = $5, duration_seconds = $6,
			        resolve_seconds = $7, include_offline = $8, scope_type = $9, scope_group_id = $10,
			        scope_tag = $11, severity = $12, enabled = $13, updated_at = now()
			  WHERE id = $1`,
			id, r.Name, r.Op, r.Threshold, r.ClearThreshold, r.DurationSeconds, r.ResolveSeconds, r.IncludeOffline,
			r.Scope.Type, r.Scope.GroupID, r.Scope.Tag, r.Severity, r.Enabled); err != nil {
			return mapRuleErr(err, "actualizando regla de alerta")
		}
		out, err = alertRuleByID(ctx, tx, id, "")
		return err
	})
	if err != nil {
		return AlertRule{}, err
	}
	return out, nil
}

// DeleteAlertRule borra la regla y devuelve su nombre. Sus alertas activas quedan sin regla y el evaluador las
// resuelve (rule_deleted). ErrNotFound si no existe.
func (s *Store) DeleteAlertRule(ctx context.Context, id string) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, `DELETE FROM alert_rules WHERE id = $1 RETURNING name`, id).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("borrando regla de alerta: %w", err)
	}
	return name, nil
}
