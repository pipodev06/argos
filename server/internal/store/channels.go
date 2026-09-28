package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	// ErrChannelExists: ya hay un canal con ese nombre (sin distinguir mayusculas).
	ErrChannelExists = errors.New("ya existe un canal con ese nombre")
	// ErrTooManyChannels: ya hay MaxChannels canales.
	ErrTooManyChannels = errors.New("demasiados canales de notificacion")
	// ErrChannelNotFound: algun canal indicado no existe.
	ErrChannelNotFound = errors.New("canal de notificacion inexistente")
	// ErrTooManyRuleChannels: una regla admite como maximo MaxChannelsPerRule canales.
	ErrTooManyRuleChannels = errors.New("demasiados canales en la regla")
)

// Topes de cardinalidad.
const (
	MaxChannels        = 50
	MaxChannelsPerRule = 20

	channelNameKey  = "notification_channels_name_lower_key"
	ruleChannelFKey = "rule_channels_channel_id_fkey"
	channelGroupKey = "notification_channels_group_id_fkey"
)

// Channel es un canal de notificacion. SecretEnc es el blob cifrado (la API nunca lo devuelve).
type Channel struct {
	ID          string
	Name        string
	Kind        string
	Config      []byte // JSON sin secretos
	SecretEnc   []byte
	SecretKeyID *string
	MinSeverity string
	Enabled     bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
	// GroupID es el dueño del canal (nil = canal global del admin); GroupName es su nombre (solo lectura).
	GroupID   *string
	GroupName *string
}

const channelSelect = `
SELECT c.id, c.name, c.kind, c.config, c.secret_enc, c.secret_key_id, c.min_severity, c.enabled, c.created_at, c.updated_at,
       c.group_id, g.name
  FROM notification_channels c LEFT JOIN host_groups g ON g.id = c.group_id`

func scanChannel(row pgx.Row) (Channel, error) {
	var c Channel
	err := row.Scan(&c.ID, &c.Name, &c.Kind, &c.Config, &c.SecretEnc, &c.SecretKeyID, &c.MinSeverity, &c.Enabled,
		&c.CreatedAt, &c.UpdatedAt, &c.GroupID, &c.GroupName)
	return c, err
}

// ListChannels devuelve los canales por nombre (hay un tope de MaxChannels).
func (s *Store) ListChannels(ctx context.Context) ([]Channel, error) {
	rows, err := s.pool.Query(ctx, channelSelect+` ORDER BY lower(c.name), c.id LIMIT $1`, MaxChannels)
	if err != nil {
		return nil, fmt.Errorf("listando canales: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Channel, error) { return scanChannel(row) })
	if err != nil {
		return nil, fmt.Errorf("leyendo canales: %w", err)
	}
	return out, nil
}

// ChannelByID devuelve un canal. ErrNotFound si no existe.
func (s *Store) ChannelByID(ctx context.Context, id string) (Channel, error) {
	return channelByID(ctx, s.pool, id, "")
}

func channelByID(ctx context.Context, q rowQuerier, id, lock string) (Channel, error) {
	c, err := scanChannel(q.QueryRow(ctx, channelSelect+` WHERE c.id = $1 `+lock, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Channel{}, ErrNotFound
	}
	if err != nil {
		return Channel{}, fmt.Errorf("leyendo canal: %w", err)
	}
	return c, nil
}

func mapChannelErr(err error, what string) error {
	if isFKViolation(err, channelGroupKey) {
		return ErrGroupNotFound
	}
	if isUniqueViolation(err, channelNameKey) {
		return ErrChannelExists
	}
	return fmt.Errorf("%s: %w", what, err)
}

// CreateChannel inserta un canal con el id ya generado (el secreto se cifro con ese id como AAD).
// ErrTooManyChannels o ErrChannelExists segun el caso.
func (s *Store) CreateChannel(ctx context.Context, c Channel, createdBy string) (Channel, error) {
	var out Channel
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('argos.channels.cap'))`); err != nil {
			return fmt.Errorf("lock de canales: %w", err)
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM notification_channels`).Scan(&n); err != nil {
			return fmt.Errorf("contando canales: %w", err)
		}
		if n >= MaxChannels {
			return ErrTooManyChannels
		}
		// Cuota por cliente (B7): un canal global (group_id NULL) nunca cuenta contra ninguna cuota.
		if c.GroupID != nil {
			if err := checkGroupQuota(ctx, tx, *c.GroupID, QuotaChannels, 1); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO notification_channels (id, name, kind, config, secret_enc, secret_key_id, min_severity, enabled, created_by, group_id)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, '')::uuid, $10)`,
			c.ID, c.Name, c.Kind, c.Config, c.SecretEnc, c.SecretKeyID, c.MinSeverity, c.Enabled, createdBy, c.GroupID); err != nil {
			return mapChannelErr(err, "insertando canal")
		}
		var err error
		out, err = channelByID(ctx, tx, c.ID, "")
		return err
	})
	if err != nil {
		return Channel{}, err
	}
	return out, nil
}

// UpdateChannel bloquea el canal, deja que mutate lo modifique (su error se devuelve tal cual) y guarda todos
// los campos editables. Si el canal queda deshabilitado, sus entregas pendientes pasan a suppressed (no
// quedan colgadas contando pa el tope de la cola). ErrNotFound o ErrChannelExists segun el caso.
func (s *Store) UpdateChannel(ctx context.Context, id string, mutate func(*Channel) error) (Channel, error) {
	var out Channel
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		c, err := channelByID(ctx, tx, id, "FOR UPDATE OF c")
		if err != nil {
			return err
		}
		if err := mutate(&c); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE notification_channels SET name = $2, config = $3, secret_enc = $4, secret_key_id = $5,
			        min_severity = $6, enabled = $7, group_id = $8, updated_at = now()
			  WHERE id = $1`,
			id, c.Name, c.Config, c.SecretEnc, c.SecretKeyID, c.MinSeverity, c.Enabled, c.GroupID); err != nil {
			return mapChannelErr(err, "actualizando canal")
		}
		if err := channelScopeViolations(ctx, tx, violationsOfChannel, id); err != nil {
			return err
		}
		if !c.Enabled {
			if _, err := tx.Exec(ctx,
				`UPDATE notification_deliveries SET status = 'suppressed', lease_until = NULL
				  WHERE channel_id = $1 AND status IN ('pending', 'sending')`, id); err != nil {
				return fmt.Errorf("suprimiendo entregas del canal: %w", err)
			}
		}
		out, err = channelByID(ctx, tx, id, "")
		return err
	})
	if err != nil {
		return Channel{}, err
	}
	return out, nil
}

// DeleteChannel borra el canal y devuelve su nombre y tipo. ErrNotFound si no existe.
func (s *Store) DeleteChannel(ctx context.Context, id string) (name, kind string, err error) {
	err = s.pool.QueryRow(ctx, `DELETE FROM notification_channels WHERE id = $1 RETURNING name, kind`, id).Scan(&name, &kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", fmt.Errorf("borrando canal: %w", err)
	}
	return name, kind, nil
}

// ChannelKeyIDs devuelve los secret_key_id distintos de los canales y de los chequeos de servicio con secreto
// (pa validar la clave al arrancar).
func (s *Store) ChannelKeyIDs(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT secret_key_id FROM notification_channels WHERE secret_enc IS NOT NULL
	 UNION SELECT secret_key_id FROM service_check_defs WHERE secret_enc IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("leyendo claves de canales: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("leyendo claves de canales: %w", err)
	}
	return ids, nil
}

// HasEnabledChannels dice si hay algun canal habilitado (el worker de entregas solo trabaja entonces).
func (s *Store) HasEnabledChannels(ctx context.Context) (bool, error) {
	var ok bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM notification_channels WHERE enabled)`).Scan(&ok); err != nil {
		return false, fmt.Errorf("consultando canales habilitados: %w", err)
	}
	return ok, nil
}

// SetRuleChannels reemplaza los canales de una regla. ErrNotFound si la regla no existe, ErrChannelNotFound si
// algun canal no existe, ErrTooManyRuleChannels si hay mas de MaxChannelsPerRule, *ChannelScopeError si un canal con
// dueño no cubre el alcance de la regla. ids ya viene sin duplicados.
func (s *Store) SetRuleChannels(ctx context.Context, ruleID string, ids []string) error {
	if len(ids) > MaxChannelsPerRule {
		return ErrTooManyRuleChannels
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := alertRuleByID(ctx, tx, ruleID, "FOR UPDATE OF r"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM rule_channels WHERE rule_id = $1`, ruleID); err != nil {
			return fmt.Errorf("limpiando canales de la regla: %w", err)
		}
		if len(ids) == 0 {
			return nil
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO rule_channels (rule_id, channel_id) SELECT $1, unnest($2::uuid[])`, ruleID, ids); err != nil {
			if isFKViolation(err, ruleChannelFKey) {
				return ErrChannelNotFound
			}
			return fmt.Errorf("asignando canales a la regla: %w", err)
		}
		return channelScopeViolations(ctx, tx, violationsOfRule, ruleID)
	})
}

// ErrChannelScopeMismatch: un canal con dueño (grupo) solo puede usarse en reglas cuyo alcance quede enteramente
// dentro de ese grupo; si no, sus notificaciones llevarian datos de otros clientes a su destino.
var ErrChannelScopeMismatch = errors.New("el canal pertenece a un grupo y la regla alcanza hosts de otros")

// RuleRef identifica una regla en un error de alcance de canal.
type RuleRef struct {
	ID   string
	Name string
}

// ChannelScopeError lleva las reglas que rompen la regla "canal con dueño = alcance dentro del dueño".
// Unwrap devuelve ErrChannelScopeMismatch.
type ChannelScopeError struct {
	Rules []RuleRef
}

func (e *ChannelScopeError) Error() string {
	return fmt.Sprintf("%v (%d reglas)", ErrChannelScopeMismatch, len(e.Rules))
}
func (e *ChannelScopeError) Unwrap() error { return ErrChannelScopeMismatch }

// channelScopeViolationsSQL lista las reglas (con su canal) cuyo alcance NO queda dentro del grupo dueño de un canal
// asignado. Una regla queda dentro de G si su alcance es el grupo G o un host/dispositivo de G; una regla
// service_check tambien si es su chequeo el que queda dentro de G (las alertas salen de la interseccion). El
// alcance `all` nunca queda dentro. Se le agrega un filtro (AND ...) opcional, ORDER BY y LIMIT.
const channelScopeViolationsSQL = `
SELECT DISTINCT r.id::text AS id, r.name AS name
  FROM rule_channels rc
  JOIN notification_channels ch ON ch.id = rc.channel_id AND ch.group_id IS NOT NULL
  JOIN alert_rules r ON r.id = rc.rule_id
  LEFT JOIN hosts rh ON rh.id = r.scope_host_id
  LEFT JOIN service_check_defs d ON d.id = r.check_id
  LEFT JOIN hosts dh ON dh.id = d.scope_host_id
 WHERE (CASE WHEN r.scope_type = 'group' THEN r.scope_group_id
             WHEN r.scope_type IN ('host', 'device') THEN rh.group_id END) IS DISTINCT FROM ch.group_id
   AND (CASE WHEN r.kind = 'service_check' THEN
               CASE WHEN d.scope_type = 'group' THEN d.scope_group_id
                    WHEN d.scope_type IN ('host', 'device') THEN dh.group_id END END) IS DISTINCT FROM ch.group_id`

// maxScopeErrorRules acota las reglas que devuelve un ChannelScopeError.
const maxScopeErrorRules = 20

// Filtros de channelScopeViolations (cada uno con un unico argumento).
const (
	violationsOfRule    = ` AND r.id = $1`
	violationsOfChannel = ` AND ch.id = $1`
	violationsOfCheck   = ` AND r.check_id = $1`
)

// channelScopeViolations devuelve un *ChannelScopeError si hay reglas fuera del dueño de sus canales (dentro de la
// transaccion que acaba de cambiar algo: quien llama la revierte devolviendo el error).
func channelScopeViolations(ctx context.Context, q queryer, filter, arg string) error {
	rows, err := q.Query(ctx, channelScopeViolationsSQL+filter+` ORDER BY 2, 1 LIMIT `+strconv.Itoa(maxScopeErrorRules), arg)
	if err != nil {
		return fmt.Errorf("comprobando alcance de canales: %w", err)
	}
	refs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (RuleRef, error) {
		var r RuleRef
		err := row.Scan(&r.ID, &r.Name)
		return r, err
	})
	if err != nil {
		return fmt.Errorf("leyendo reglas fuera de alcance: %w", err)
	}
	if len(refs) > 0 {
		return &ChannelScopeError{Rules: refs}
	}
	return nil
}

// ChannelScopeMismatches cuenta las reglas con un canal de otro dueño (datos anteriores a la validacion o creados
// al mover un host de grupo). Diagnostico: no corrige nada; el encolado de entregas ya no les envia.
func (s *Store) ChannelScopeMismatches(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*)::int FROM (`+channelScopeViolationsSQL+`) v`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("contando reglas con canal de otro grupo: %w", err)
	}
	return n, nil
}
