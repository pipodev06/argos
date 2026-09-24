package store

import (
	"context"
	"errors"
	"fmt"
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
}

const channelSelect = `
SELECT id, name, kind, config, secret_enc, secret_key_id, min_severity, enabled, created_at, updated_at
  FROM notification_channels`

func scanChannel(row pgx.Row) (Channel, error) {
	var c Channel
	err := row.Scan(&c.ID, &c.Name, &c.Kind, &c.Config, &c.SecretEnc, &c.SecretKeyID, &c.MinSeverity, &c.Enabled,
		&c.CreatedAt, &c.UpdatedAt)
	return c, err
}

// ListChannels devuelve los canales por nombre (hay un tope de MaxChannels).
func (s *Store) ListChannels(ctx context.Context) ([]Channel, error) {
	rows, err := s.pool.Query(ctx, channelSelect+` ORDER BY lower(name), id LIMIT $1`, MaxChannels)
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
	c, err := scanChannel(q.QueryRow(ctx, channelSelect+` WHERE id = $1 `+lock, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Channel{}, ErrNotFound
	}
	if err != nil {
		return Channel{}, fmt.Errorf("leyendo canal: %w", err)
	}
	return c, nil
}

func mapChannelErr(err error, what string) error {
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
		if _, err := tx.Exec(ctx,
			`INSERT INTO notification_channels (id, name, kind, config, secret_enc, secret_key_id, min_severity, enabled, created_by)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, '')::uuid)`,
			c.ID, c.Name, c.Kind, c.Config, c.SecretEnc, c.SecretKeyID, c.MinSeverity, c.Enabled, createdBy); err != nil {
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
		c, err := channelByID(ctx, tx, id, "FOR UPDATE")
		if err != nil {
			return err
		}
		if err := mutate(&c); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE notification_channels SET name = $2, config = $3, secret_enc = $4, secret_key_id = $5,
			        min_severity = $6, enabled = $7, updated_at = now()
			  WHERE id = $1`,
			id, c.Name, c.Config, c.SecretEnc, c.SecretKeyID, c.MinSeverity, c.Enabled); err != nil {
			return mapChannelErr(err, "actualizando canal")
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
// algun canal no existe, ErrTooManyRuleChannels si hay mas de MaxChannelsPerRule. ids ya viene sin duplicados.
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
		return nil
	})
}
