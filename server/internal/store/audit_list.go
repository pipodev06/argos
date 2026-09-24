package store

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"time"
)

// Limites de la lista de auditoria.
const (
	DefaultAuditListLimit = 50
	MaxAuditListLimit     = 100
	// MaxAuditEventNames acota GET /audit/events (hay ~40 eventos; el tope protege de un evento con nombre libre).
	MaxAuditEventNames = 200
)

// AuditCursor es la posicion keyset de la ultima fila de una pagina, en el orden (ts DESC, id DESC).
type AuditCursor struct {
	TS time.Time
	ID int64
}

// AuditFilter son los filtros y la paginacion de ListAudit. Quien llama valida formatos y rangos.
type AuditFilter struct {
	Events   []string   // vacio = todos; varios con OR
	Outcomes []string   // vacio = todos; varios con OR
	Actor    string     // subcadena de actor_username, sin distinguir mayusculas
	From     *time.Time // ts >= From
	To       *time.Time // ts <= To
	Limit    int
	After    *AuditCursor
}

// AuditRow es un evento tal como esta guardado (Detail ya saneado por la lista blanca al escribir).
type AuditRow struct {
	ID            int64
	TS            time.Time
	Event         string
	Outcome       string
	ActorUserID   *string
	ActorUsername *string
	TargetType    *string
	TargetID      *string
	IP            *netip.Addr
	Detail        []byte // JSON o nil
}

// AuditPage es una pagina de auditoria: Total cuenta todos los eventos que cumplen los filtros.
type AuditPage struct {
	Items      []AuditRow
	NextCursor *AuditCursor // nil = era la ultima pagina
	Total      int
}

// ListAudit devuelve una pagina de auditoria (solo lectura), de la mas reciente a la mas antigua.
func (s *Store) ListAudit(ctx context.Context, f AuditFilter) (AuditPage, error) {
	limit := min(max(f.Limit, 1), MaxAuditListLimit)

	var args argList
	var where []string
	if len(f.Events) > 0 {
		where = append(where, fmt.Sprintf(`event = ANY(%s::text[])`, args.add(f.Events)))
	}
	if len(f.Outcomes) > 0 {
		where = append(where, fmt.Sprintf(`outcome = ANY(%s::text[])`, args.add(f.Outcomes)))
	}
	if f.Actor != "" {
		where = append(where, fmt.Sprintf(`strpos(lower(actor_username), lower(%s)) > 0`, args.add(f.Actor)))
	}
	if f.From != nil {
		where = append(where, fmt.Sprintf(`ts >= %s::timestamptz`, args.add(*f.From)))
	}
	if f.To != nil {
		where = append(where, fmt.Sprintf(`ts <= %s::timestamptz`, args.add(*f.To)))
	}
	whereSQL := "TRUE"
	if len(where) > 0 {
		whereSQL = strings.Join(where, " AND ")
	}
	totalArgs := append(argList(nil), args...)

	pageSQL := whereSQL
	if c := f.After; c != nil {
		pageSQL += fmt.Sprintf(` AND (ts, id) < (%s::timestamptz, %s::bigint)`, args.add(c.TS), args.add(c.ID))
	}

	// Los fragmentos SQL son fijos; los valores del cliente van siempre como parametros.
	query := fmt.Sprintf(`
SELECT id, ts, event, outcome, actor_user_id::text, actor_username, target_type, target_id, ip, detail
  FROM audit_log
 WHERE %s
 ORDER BY ts DESC, id DESC
 LIMIT %d`, pageSQL, limit+1)

	rows, err := s.pool.Query(ctx, query, []any(args)...)
	if err != nil {
		return AuditPage{}, fmt.Errorf("listando auditoria: %w", err)
	}
	defer rows.Close()

	page := AuditPage{Items: []AuditRow{}}
	for rows.Next() {
		var r AuditRow
		if err := rows.Scan(&r.ID, &r.TS, &r.Event, &r.Outcome, &r.ActorUserID, &r.ActorUsername,
			&r.TargetType, &r.TargetID, &r.IP, &r.Detail); err != nil {
			return AuditPage{}, fmt.Errorf("leyendo evento de auditoria: %w", err)
		}
		page.Items = append(page.Items, r)
	}
	if err := rows.Err(); err != nil {
		return AuditPage{}, fmt.Errorf("iterando auditoria: %w", err)
	}
	rows.Close()

	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		last := page.Items[limit-1]
		page.NextCursor = &AuditCursor{TS: last.TS, ID: last.ID}
	}

	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE `+whereSQL, []any(totalArgs)...).Scan(&page.Total); err != nil {
		return AuditPage{}, fmt.Errorf("contando auditoria: %w", err)
	}
	return page, nil
}

// AuditEvents devuelve los nombres de evento distintos que existen, ordenados y acotados.
func (s *Store) AuditEvents(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT event FROM audit_log ORDER BY event LIMIT $1`, MaxAuditEventNames)
	if err != nil {
		return nil, fmt.Errorf("listando eventos de auditoria: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return nil, fmt.Errorf("leyendo evento: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterando eventos: %w", err)
	}
	return out, nil
}
