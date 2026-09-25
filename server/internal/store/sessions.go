package store

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"
)

// Session es una sesion de dashboard recien creada. Solo se guarda el hash del ID.
type Session struct {
	IDHash    []byte // SHA-256 del ID de la cookie
	UserID    string
	CreatedAt time.Time
	ExpiresAt time.Time // limite absoluto
	IP        *netip.Addr
	UserAgent string // ya truncado por quien llama
}

// SessionInfo es una sesion existente junto con su usuario.
type SessionInfo struct {
	IDHash     []byte
	LastSeenAt time.Time
	ExpiresAt  time.Time
	User       User
}

// CreateSession guarda una sesion nueva.
func (s *Store) CreateSession(ctx context.Context, sess Session) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO sessions (id_hash, user_id, created_at, last_seen_at, expires_at, ip, user_agent)
		 VALUES ($1, $2, $3, $3, $4, $5, $6)`,
		sess.IDHash, sess.UserID, sess.CreatedAt, sess.ExpiresAt, sess.IP, sess.UserAgent)
	if err != nil {
		return fmt.Errorf("creando sesion: %w", err)
	}
	return nil
}

// SessionByHash busca una sesion por el hash de su ID, con su usuario. ErrNotFound si no existe.
// No valida expiracion ni usuario deshabilitado: eso lo decide quien llama con su reloj.
func (s *Store) SessionByHash(ctx context.Context, idHash []byte) (SessionInfo, error) {
	var si SessionInfo
	si.IDHash = idHash
	u := &si.User
	err := s.pool.QueryRow(ctx,
		`SELECT s.last_seen_at, s.expires_at,
		        u.id, u.username, u.role, u.must_change_password, u.disabled_at,
		        u.all_groups, u.can_inventory, u.can_updates,
		        ARRAY(SELECT ug.group_id::text FROM user_groups ug WHERE ug.user_id = u.id ORDER BY ug.group_id)
		   FROM sessions s JOIN users u ON u.id = s.user_id
		  WHERE s.id_hash = $1`, idHash,
	).Scan(&si.LastSeenAt, &si.ExpiresAt, &u.ID, &u.Username, &u.Role, &u.MustChangePassword, &u.DisabledAt,
		&u.AllGroups, &u.CanInventory, &u.CanUpdates, &u.GroupIDs)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionInfo{}, ErrNotFound
	}
	if err != nil {
		return SessionInfo{}, fmt.Errorf("buscando sesion: %w", err)
	}
	return si, nil
}

// TouchSession actualiza last_seen_at.
func (s *Store) TouchSession(ctx context.Context, idHash []byte, now time.Time) error {
	if _, err := s.pool.Exec(ctx, `UPDATE sessions SET last_seen_at = $2 WHERE id_hash = $1`, idHash, now); err != nil {
		return fmt.Errorf("actualizando sesion: %w", err)
	}
	return nil
}

// DeleteSession borra una sesion (logout o sesion vencida). No es error que no exista.
func (s *Store) DeleteSession(ctx context.Context, idHash []byte) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE id_hash = $1`, idHash); err != nil {
		return fmt.Errorf("borrando sesion: %w", err)
	}
	return nil
}
