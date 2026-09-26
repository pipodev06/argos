// Package store es el acceso a Postgres de argos: pool, migraciones y consultas.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Errores de dominio. Quien llama los compara con errors.Is.
var (
	// ErrNotFound: no existe la fila buscada (token de agente o de enroll).
	ErrNotFound = errors.New("no encontrado")
	// ErrTokenUsed: el enroll token ya fue consumido.
	ErrTokenUsed = errors.New("enroll token ya usado")
	// ErrTokenExpired: el enroll token paso su fecha de expiracion.
	ErrTokenExpired = errors.New("enroll token expirado")
	// ErrTooManyTokens: ya hay MaxActiveEnrollTokens enroll tokens vigentes sin usar.
	ErrTooManyTokens = errors.New("demasiados enroll tokens activos")
	// ErrPartitionMissing: el ts de la muestra no cae en ninguna particion de metrics/service_checks
	// (el mantenimiento no las creo o no alcanzo a crearlas). Es un problema del servidor y
	// reintentable, no de la muestra.
	ErrPartitionMissing = errors.New("no hay particion para el timestamp")

	// Grupos y etiquetas.

	// ErrGroupNotFound: el grupo referenciado (en el body) no existe.
	ErrGroupNotFound = errors.New("grupo no encontrado")
	// ErrGroupExists: ya hay un grupo con ese nombre (sin distinguir mayusculas).
	ErrGroupExists = errors.New("ya existe un grupo con ese nombre")
	// ErrGroupNotEmpty: el grupo tiene hosts y no se puede borrar.
	ErrGroupNotEmpty = errors.New("el grupo tiene hosts")
	// ErrTooManyGroups: ya hay MaxGroups grupos.
	ErrTooManyGroups = errors.New("demasiados grupos")
	// ErrUnknownHosts: alguno de los hosts pedidos no existe.
	ErrUnknownHosts = errors.New("algun host no existe")
)

// MaxGroups es el maximo de grupos.
const MaxGroups = 200

// sqlstateCheckViolation es 23514. Un INSERT sin particion destino falla con este codigo pero sin
// nombre de constraint; un CHECK real (p. ej. service_checks_status_check) si lo trae.
const sqlstateCheckViolation = "23514"

// isPartitionMissing reconoce el error de "no se encontro particion" de Postgres.
func isPartitionMissing(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == sqlstateCheckViolation && pgErr.ConstraintName == ""
}

// isUniqueViolation reconoce 23505 sobre el indice/constraint dado.
func isUniqueViolation(err error, constraint string) bool {
	return isPgConstraint(err, "23505", constraint)
}

// isFKViolation reconoce la violacion de la FK dada: 23503 (foreign_key_violation) o 23001
// (restrict_violation, que es lo que devuelve ON DELETE RESTRICT).
func isFKViolation(err error, constraint string) bool {
	return isPgConstraint(err, "23503", constraint) || isPgConstraint(err, "23001", constraint)
}

func isPgConstraint(err error, code, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code && pgErr.ConstraintName == constraint
}

// MaxActiveEnrollTokens acota los enroll tokens vigentes (sin usar y sin expirar) a la vez.
const MaxActiveEnrollTokens = 50

// Store implementa las consultas sobre un pool de pgx.
type Store struct {
	pool *pgxpool.Pool
	// retention son los valores de arranque de la retencion de metricas (WithRetention); el detalle global
	// puede sobrescribirse desde la API (tabla settings).
	retention RetentionDefaults
}

// New envuelve un pool ya abierto (y migrado).
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, retention: DefaultRetention()}
}

// Ping comprueba que Postgres responde (lo usa GET /health; el llamador acota el tiempo con el contexto).
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// CreateEnrollToken guarda el hash SHA-256 de un enroll token (nunca el token en claro)
// ligado al usuario que lo creo y, opcionalmente, al grupo con el que quedara el host enrolado
// (ErrGroupNotFound si no existe). Devuelve ErrTooManyTokens si ya hay MaxActiveEnrollTokens
// vigentes; el lock advisory serializa creaciones concurrentes pa que el tope sea exacto.
func (s *Store) CreateEnrollToken(ctx context.Context, tokenHash, hostname, createdBy string, groupID *string, expiresAt time.Time) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('argos.enroll_tokens.cap'))`); err != nil {
			return fmt.Errorf("lock de enroll tokens: %w", err)
		}
		var active int
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM enroll_tokens WHERE used_at IS NULL AND expires_at > now()`,
		).Scan(&active); err != nil {
			return fmt.Errorf("contando enroll tokens activos: %w", err)
		}
		if active >= MaxActiveEnrollTokens {
			return ErrTooManyTokens
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO enroll_tokens (token_hash, hostname, created_by, group_id, expires_at) VALUES ($1, $2, $3, $4, $5)`,
			tokenHash, hostname, createdBy, groupID, expiresAt); err != nil {
			if isFKViolation(err, "enroll_tokens_group_id_fkey") {
				return ErrGroupNotFound
			}
			return fmt.Errorf("insertando enroll token: %w", err)
		}
		return nil
	})
}

// EnrollHost consume el enroll token y crea el host en una sola transaccion.
// Devuelve ErrNotFound, ErrTokenUsed o ErrTokenExpired segun el estado del token.
// El FOR UPDATE serializa consumos concurrentes del mismo token.
func (s *Store) EnrollHost(ctx context.Context, enrollTokenHash, osName, arch, agentTokenHash string) (string, error) {
	var hostID string
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var hostname string
		var groupID *string
		var used, expired bool
		err := tx.QueryRow(ctx,
			`SELECT hostname, group_id, used_at IS NOT NULL, expires_at <= now()
			   FROM enroll_tokens WHERE token_hash = $1 FOR UPDATE`,
			enrollTokenHash,
		).Scan(&hostname, &groupID, &used, &expired)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("leyendo enroll token: %w", err)
		}
		if used {
			return ErrTokenUsed
		}
		if expired {
			return ErrTokenExpired
		}

		if err := tx.QueryRow(ctx,
			`INSERT INTO hosts (hostname, os, arch, agent_token_hash, status, last_seen_at, group_id)
			 VALUES ($1, $2, $3, $4, 'up', now(), $5) RETURNING id`,
			hostname, osName, arch, agentTokenHash, groupID,
		).Scan(&hostID); err != nil {
			return fmt.Errorf("creando host: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO host_events (host_id, from_status, to_status, source) VALUES ($1, NULL, 'up', 'enroll')`, hostID); err != nil {
			return fmt.Errorf("registrando evento inicial del host: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE enroll_tokens SET used_at = now() WHERE token_hash = $1`, enrollTokenHash); err != nil {
			return fmt.Errorf("marcando enroll token usado: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return hostID, nil
}

// HostIDByTokenHash busca el host dueno de un agent token (por su hash SHA-256).
// Devuelve ErrNotFound si ningun host lo tiene o si el token fue revocado / el host
// esta deshabilitado.
func (s *Store) HostIDByTokenHash(ctx context.Context, tokenHash string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `SELECT id FROM hosts
		  WHERE kind = 'agent' AND agent_token_hash = $1 AND agent_token_revoked_at IS NULL AND status <> 'disabled'`, tokenHash).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("buscando host por token: %w", err)
	}
	return id, nil
}

// disableHostsSQL deshabilita (y revoca el token de) los hosts que aun no lo estan y registra la transicion en
// host_events (source admin) en la misma sentencia. El FOR UPDATE fija el status previo real.
const disableHostsSQL = `
WITH upd AS (
  UPDATE hosts h SET status = 'disabled', agent_token_revoked_at = COALESCE(h.agent_token_revoked_at, now())
    FROM (SELECT id, status FROM hosts WHERE id = ANY($1) AND status <> 'disabled' ORDER BY id FOR UPDATE) prev
   WHERE h.id = prev.id
  RETURNING h.id, prev.status AS prev_status
), ev AS (
  INSERT INTO host_events (host_id, from_status, to_status, source)
  SELECT id, prev_status, 'disabled', 'admin' FROM upd
)
SELECT count(*) FROM upd`

// RevokeHost revoca el token del agente y deshabilita el host. Es idempotente:
// una segunda llamada conserva la fecha de la primera. Devuelve ErrNotFound si no existe.
func (s *Store) RevokeHost(ctx context.Context, hostID string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM hosts WHERE id = $1)`, hostID).Scan(&exists); err != nil {
			return fmt.Errorf("buscando host: %w", err)
		}
		if !exists {
			return ErrNotFound
		}
		if _, err := tx.Exec(ctx, disableHostsSQL, []string{hostID}); err != nil {
			return fmt.Errorf("revocando host: %w", err)
		}
		return nil
	})
}

// PendingEnrollToken es un enroll token vigente sin usar. Nunca lleva el token ni su hash: ID es un identificador
// aparte (enroll_tokens.id) pa listar y revocar.
type PendingEnrollToken struct {
	ID        string
	Hostname  string
	Group     *GroupRef
	CreatedBy *string // username; nil si el usuario ya no existe
	CreatedAt time.Time
	ExpiresAt time.Time
}

// ListPendingEnrollTokens devuelve los tokens sin usar y sin expirar, los mas recientes por vencer primero (hay un
// tope natural de MaxActiveEnrollTokens).
func (s *Store) ListPendingEnrollTokens(ctx context.Context) ([]PendingEnrollToken, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT t.id, t.hostname, g.id, g.name, u.username, t.created_at, t.expires_at
		   FROM enroll_tokens t
		   LEFT JOIN host_groups g ON g.id = t.group_id
		   LEFT JOIN users u ON u.id = t.created_by
		  WHERE t.used_at IS NULL AND t.expires_at > now()
		  ORDER BY t.expires_at DESC, t.id LIMIT $1`, MaxActiveEnrollTokens)
	if err != nil {
		return nil, fmt.Errorf("listando enroll tokens pendientes: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (PendingEnrollToken, error) {
		var t PendingEnrollToken
		var gid, gname *string
		if err := row.Scan(&t.ID, &t.Hostname, &gid, &gname, &t.CreatedBy, &t.CreatedAt, &t.ExpiresAt); err != nil {
			return t, err
		}
		if gid != nil && gname != nil {
			t.Group = &GroupRef{ID: *gid, Name: *gname}
		}
		return t, nil
	})
	if err != nil {
		return nil, fmt.Errorf("leyendo enroll tokens pendientes: %w", err)
	}
	return out, nil
}

// RevokeEnrollToken borra un enroll token pendiente y devuelve su hostname. ErrNotFound si no existe, ya se uso o
// expiro (un token usado ya dio origen a un host: se revoca el host, no el token).
func (s *Store) RevokeEnrollToken(ctx context.Context, id string) (hostname string, err error) {
	err = s.pool.QueryRow(ctx,
		`DELETE FROM enroll_tokens WHERE id = $1 AND used_at IS NULL AND expires_at > now() RETURNING hostname`, id).Scan(&hostname)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("revocando enroll token: %w", err)
	}
	return hostname, nil
}
