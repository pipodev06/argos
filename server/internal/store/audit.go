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
	// TargetLabel es el nombre legible del objetivo (vacio = se resuelve al insertar: nombre actual,
	// el del detalle o el ultimo guardado para ese objetivo). Queda guardado aunque el objeto se borre.
	TargetLabel string
	IP          *netip.Addr
	Detail      []byte // JSON o nil
}

// targetCurrentNameSQL es el nombre ACTUAL del objetivo segun su tipo (NULL si ya no existe).
// typ e id son expresiones SQL de texto. Las tablas de objetivos son chicas: la subconsulta es barata.
func targetCurrentNameSQL(typ, id string) string {
	return fmt.Sprintf(`(CASE %[1]s
    WHEN 'host'          THEN (SELECT hostname FROM hosts WHERE id::text = %[2]s)
    WHEN 'device'        THEN (SELECT hostname FROM hosts WHERE id::text = %[2]s)
    WHEN 'user'          THEN (SELECT username FROM users WHERE id::text = %[2]s)
    WHEN 'group'         THEN (SELECT name FROM host_groups WHERE id::text = %[2]s)
    WHEN 'alert_rule'    THEN (SELECT name FROM alert_rules WHERE id::text = %[2]s)
    WHEN 'channel'       THEN (SELECT name FROM notification_channels WHERE id::text = %[2]s)
    WHEN 'maintenance'   THEN (SELECT name FROM maintenance_windows WHERE id::text = %[2]s)
    WHEN 'service_check' THEN (SELECT name FROM service_check_defs WHERE id::text = %[2]s)
    WHEN 'snmp_profile'  THEN (SELECT name FROM snmp_profiles WHERE id::text = %[2]s)
  END)`, typ, id)
}

// targetStoredNameSQL es el nombre que el propio evento conoce aunque el objeto ya no exista: el
// del detalle (name/hostname/username) o el ultimo guardado en un evento anterior del mismo objetivo
// (indice audit_log_target_idx).
func targetStoredNameSQL(typ, id, detail string) string {
	return fmt.Sprintf(`COALESCE(%[3]s->>'name', %[3]s->>'hostname', %[3]s->>'username',
    (SELECT p.target_label FROM audit_log p
      WHERE p.target_type = %[1]s AND p.target_id = %[2]s AND p.target_label IS NOT NULL
      ORDER BY p.id DESC LIMIT 1))`, typ, id, detail)
}

// InsertAudit registra un evento. Los campos vacios se guardan como NULL.
func (s *Store) InsertAudit(ctx context.Context, e AuditEntry) error {
	var detail any
	if len(e.Detail) > 0 {
		detail = e.Detail
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO audit_log (actor_user_id, actor_username, event, outcome, target_type, target_id, ip, detail, target_label)
		 VALUES (NULLIF($1, '')::uuid, NULLIF($2, ''), $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7, $8::jsonb,
		   CASE WHEN NULLIF($5, '') IS NULL THEN NULL ELSE left(COALESCE(NULLIF($9, ''), `+
			targetCurrentNameSQL("$5::text", "$6::text")+`, `+targetStoredNameSQL("$5::text", "$6::text", "$8::jsonb")+`), 128) END)`,
		e.ActorUserID, e.ActorUsername, e.Event, e.Outcome, e.TargetType, e.TargetID, e.IP, detail, e.TargetLabel)
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
