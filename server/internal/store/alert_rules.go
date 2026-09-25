package store

import (
	"context"
	"encoding/json"
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
	MaxExpectedPorts     = 100
	MaxAlertRules        = 200
	MaxActiveMaintenance = 50
)

// Tipos de alcance de una regla o de una ventana de mantenimiento.
const (
	ScopeAll   = "all"
	ScopeGroup = "group"
)

// AlertScope es el alcance: todos los hosts o los de un grupo.
type AlertScope struct {
	Type    string
	GroupID *string
}

// ExpectedPort es un puerto que una regla exposed_port ignora (proto tcp|udp).
type ExpectedPort struct {
	Proto string `json:"proto"`
	Port  int    `json:"port"`
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
	// CheckID (solo kind service_check) es el chequeo que vigila la regla; CheckName es su nombre (solo lectura).
	CheckID   *string
	CheckName *string
	// WarnOrDown (solo service_check): tambien incumple con warn, no solo con down.
	WarnOrDown bool
	// ExpectedPorts (solo kind exposed_port): puertos que la regla ignora (max MaxExpectedPorts).
	ExpectedPorts []ExpectedPort
	Scope         AlertScope
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
       r.include_offline, r.scope_type, r.scope_group_id, g.name, r.check_id, cd.name, r.warn_is_breach, r.expected_ports,
       r.severity, r.enabled, r.created_at, r.updated_at,
       (SELECT count(*)::int FROM alerts a WHERE a.rule_id = r.id AND a.state <> 'resolved'),
       COALESCE((SELECT array_agg(rc.channel_id::text ORDER BY rc.channel_id) FROM rule_channels rc WHERE rc.rule_id = r.id), '{}')
  FROM alert_rules r LEFT JOIN host_groups g ON g.id = r.scope_group_id
  LEFT JOIN service_check_defs cd ON cd.id = r.check_id`

func scanAlertRule(row pgx.Row) (AlertRule, error) {
	var r AlertRule
	var expected []byte
	err := row.Scan(&r.ID, &r.Name, &r.Kind, &r.Metric, &r.Op, &r.Threshold, &r.ClearThreshold, &r.DurationSeconds,
		&r.ResolveSeconds, &r.IncludeOffline, &r.Scope.Type, &r.Scope.GroupID,
		&r.GroupName, &r.CheckID, &r.CheckName, &r.WarnOrDown, &expected, &r.Severity, &r.Enabled, &r.CreatedAt, &r.UpdatedAt, &r.ActiveAlerts, &r.ChannelIDs)
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(expected, &r.ExpectedPorts); err != nil {
		return r, fmt.Errorf("leyendo expected_ports: %w", err)
	}
	return r, nil
}

// Nombres de constraint que se traducen a errores de dominio.
const (
	ruleNameKey     = "alert_rules_name_lower_key"
	ruleGroupFKey   = "alert_rules_scope_group_id_fkey"
	windowGroupFKey = "maintenance_windows_scope_group_id_fkey"
)

// ListAlertRules devuelve las reglas por nombre (hay un tope de MaxAlertRules). Un ambito restringido solo ve las
// de alcance `all` y las de sus grupos, sin canales y con ActiveAlerts contando solo alertas de sus hosts.
func (s *Store) ListAlertRules(ctx context.Context, scope Scope) ([]AlertRule, error) {
	args := argList{MaxAlertRules}
	rows, err := s.pool.Query(ctx,
		alertRuleSelect+` WHERE `+scope.configCond("r.scope_type", "r.scope_group_id", &args)+` ORDER BY lower(r.name), r.id LIMIT $1`,
		[]any(args)...)
	if err != nil {
		return nil, fmt.Errorf("listando reglas de alerta: %w", err)
	}
	rules, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (AlertRule, error) { return scanAlertRule(row) })
	if err != nil {
		return nil, fmt.Errorf("leyendo reglas de alerta: %w", err)
	}
	if scope.All() {
		return rules, nil
	}
	if err := s.narrowRules(ctx, scope, rules); err != nil {
		return nil, err
	}
	return rules, nil
}

// AlertRuleByID devuelve una regla. ErrNotFound si no existe o queda fuera del ambito. Con ambito restringido
// se aplica lo mismo que en ListAlertRules (sin canales, alertas activas solo de sus hosts).
func (s *Store) AlertRuleByID(ctx context.Context, scope Scope, id string) (AlertRule, error) {
	r, err := alertRuleByID(ctx, s.pool, id, "")
	if err != nil {
		return AlertRule{}, err
	}
	if scope.All() {
		return r, nil
	}
	if !scope.sees(r.Scope) {
		return AlertRule{}, ErrNotFound
	}
	rules := []AlertRule{r}
	if err := s.narrowRules(ctx, scope, rules); err != nil {
		return AlertRule{}, err
	}
	return rules[0], nil
}

// narrowRules recorta, en su sitio, lo que un ambito restringido no debe ver de las reglas: los canales de
// notificacion (son de admin) y las alertas activas de hosts ajenos (ActiveAlerts pasa a contar solo las suyas).
func (s *Store) narrowRules(ctx context.Context, scope Scope, rules []AlertRule) error {
	ids := make([]string, len(rules))
	for i := range rules {
		ids[i] = rules[i].ID
		rules[i].ChannelIDs = []string{}
		rules[i].ActiveAlerts = 0
	}
	args := argList{ids}
	rows, err := s.pool.Query(ctx,
		`SELECT a.rule_id::text, count(*)::int FROM alerts a
		  WHERE a.state <> 'resolved' AND a.rule_id = ANY($1::uuid[]) AND `+scope.hostCond("a.host_id", &args)+`
		  GROUP BY a.rule_id`, []any(args)...)
	if err != nil {
		return fmt.Errorf("contando alertas activas del ambito: %w", err)
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return fmt.Errorf("leyendo alertas activas del ambito: %w", err)
		}
		counts[id] = n
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterando alertas activas del ambito: %w", err)
	}
	for i := range rules {
		rules[i].ActiveAlerts = counts[rules[i].ID]
	}
	return nil
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
		expected, err := marshalExpectedPorts(r.ExpectedPorts)
		if err != nil {
			return err
		}
		var id string
		err = tx.QueryRow(ctx,
			`INSERT INTO alert_rules (name, kind, metric, op, threshold, clear_threshold, duration_seconds, resolve_seconds,
			                          include_offline, scope_type, scope_group_id,
			                          severity, enabled, created_by, check_id, warn_is_breach, expected_ports)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NULLIF($14, '')::uuid, $15, $16, $17)
			 RETURNING id`,
			r.Name, r.Kind, r.Metric, r.Op, r.Threshold, r.ClearThreshold, r.DurationSeconds, r.ResolveSeconds,
			r.IncludeOffline, r.Scope.Type, r.Scope.GroupID,
			r.Severity, r.Enabled, createdBy, r.CheckID, r.WarnOrDown, expected).Scan(&id)
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

// marshalExpectedPorts serializa la lista (nil = arreglo vacio, nunca null: la columna es NOT NULL).
func marshalExpectedPorts(p []ExpectedPort) ([]byte, error) {
	if p == nil {
		p = []ExpectedPort{}
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("serializando expected_ports: %w", err)
	}
	return b, nil
}

func mapRuleErr(err error, what string) error {
	switch {
	case isUniqueViolation(err, ruleNameKey):
		return ErrRuleExists
	case isFKViolation(err, ruleGroupFKey):
		return ErrGroupNotFound
	case isFKViolation(err, ruleCheckFKey):
		return ErrCheckNotFound
	}
	return fmt.Errorf("%s: %w", what, err)
}

// UpdateAlertRule bloquea la regla, deja que mutate la modifique (validando; su error se devuelve tal cual) y
// guarda los campos editables (no kind, metric ni check_id; si expected_ports). ErrNotFound, ErrRuleExists o ErrGroupNotFound segun el caso.
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
		expected, err := marshalExpectedPorts(r.ExpectedPorts)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE alert_rules SET name = $2, op = $3, threshold = $4, clear_threshold = $5, duration_seconds = $6,
			        resolve_seconds = $7, include_offline = $8, scope_type = $9, scope_group_id = $10,
			        severity = $11, enabled = $12, warn_is_breach = $13, expected_ports = $14, updated_at = now()
			  WHERE id = $1`,
			id, r.Name, r.Op, r.Threshold, r.ClearThreshold, r.DurationSeconds, r.ResolveSeconds, r.IncludeOffline,
			r.Scope.Type, r.Scope.GroupID, r.Severity, r.Enabled, r.WarnOrDown, expected); err != nil {
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
