package store

import (
	"context"
	"fmt"
	"net/netip"
	"time"
)

// Resultados validos de un evento de auditoria (CHECK de la migracion 00004).
const (
	OutcomeOK     = "ok"
	OutcomeFail   = "fail"
	OutcomeDenied = "denied"
)

// AuditEntry es un evento de auditoria. Quien llama es responsable de que ningun
// campo (incluido Detail, JSON ya serializado) lleve secretos ni PII innecesaria.
type AuditEntry struct {
	ActorUserID   string // "" = sin actor (login fallido de usuario inexistente, CLI, agentes)
	ActorUsername string
	Event         string
	Outcome       string
	TargetType    string
	TargetID      string
	IP            *netip.Addr
	Detail        []byte // JSON o nil
}

// InsertAudit registra un evento. Los campos vacios se guardan como NULL.
func (s *Store) InsertAudit(ctx context.Context, e AuditEntry) error {
	var detail any
	if len(e.Detail) > 0 {
		detail = e.Detail
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO audit_log (actor_user_id, actor_username, event, outcome, target_type, target_id, ip, detail)
		 VALUES (NULLIF($1, '')::uuid, NULLIF($2, ''), $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7, $8)`,
		e.ActorUserID, e.ActorUsername, e.Event, e.Outcome, e.TargetType, e.TargetID, e.IP, detail)
	if err != nil {
		return fmt.Errorf("insertando evento de auditoria: %w", err)
	}
	return nil
}

// Purga en lotes (cada tanda es su propia sentencia: no retiene locks largos ni
// genera una transaccion enorme). Devuelven cuantas filas borraron; quien llama
// repite mientras devuelvan el tamano del lote.

// PurgeSessions borra sesiones vencidas por limite absoluto o por inactividad.
func (s *Store) PurgeSessions(ctx context.Context, now time.Time, idle time.Duration, batch int) (int, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM sessions WHERE id_hash IN (
		   SELECT id_hash FROM sessions WHERE expires_at < $1 OR last_seen_at < $2 LIMIT $3)`,
		now, now.Add(-idle), batch)
	if err != nil {
		return 0, fmt.Errorf("purgando sesiones: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// PurgeAudit borra eventos anteriores a before. El trigger de la tabla solo permite
// borrar filas de mas de 30 dias.
func (s *Store) PurgeAudit(ctx context.Context, before time.Time, batch int) (int, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM audit_log WHERE id IN (SELECT id FROM audit_log WHERE ts < $1 ORDER BY id LIMIT $2)`,
		before, batch)
	if err != nil {
		return 0, fmt.Errorf("purgando auditoria: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// PurgeEnrollTokens borra enroll tokens que vencieron antes de before.
func (s *Store) PurgeEnrollTokens(ctx context.Context, before time.Time, batch int) (int, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM enroll_tokens WHERE token_hash IN (
		   SELECT token_hash FROM enroll_tokens WHERE expires_at < $1 LIMIT $2)`,
		before, batch)
	if err != nil {
		return 0, fmt.Errorf("purgando enroll tokens: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
