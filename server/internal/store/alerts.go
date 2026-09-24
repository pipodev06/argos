package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Limites del listado de alertas y eventos.
const (
	DefaultAlertListLimit = 50
	MaxAlertListLimit     = 200
	// alertDetailEvents es cuantos eventos recientes del host acompanan al detalle de una alerta.
	alertDetailEvents = 20
)

// Alert es una alerta con el hostname de su host. Los campos de la regla son el snapshot al abrirla.
type Alert struct {
	ID        string
	RuleID    *string
	RuleName  string
	RuleKind  string
	Metric    *string
	Threshold *float64
	Severity  string
	State     string
	OpenedAt  time.Time

	TriggerValue *float64
	PeakValue    *float64
	Suppressed   bool

	HostID   string
	Hostname string

	AcknowledgedByName *string
	AcknowledgedAt     *time.Time
	ResolvedAt         *time.Time
	ResolvedByName     *string
	ResolveReason      *string
}

// AlertCursor es la posicion keyset (opened_at, id) tras la ultima alerta de una pagina.
type AlertCursor struct {
	OpenedAt time.Time
	ID       string
}

// AlertFilter son los filtros de ListAlerts: cada lista vacia = sin filtro (dentro de una lista es OR).
type AlertFilter struct {
	States, Severities, HostIDs, RuleIDs []string
	After                                *AlertCursor
	Limit                                int
}

// AlertPage es una pagina de alertas; Total cuenta las que cumplen los filtros (sin cursor).
type AlertPage struct {
	Items      []Alert
	NextCursor *AlertCursor
	Total      int
}

// Delivery es el estado de una notificacion de una alerta (sin secretos ni contenido del canal).
type Delivery struct {
	ID          int64
	ChannelID   string
	ChannelName string
	Event       string
	Seq         int
	Status      string
	Attempts    int
	CreatedAt   time.Time
	SentAt      *time.Time
}

// HostEvent es una transicion de status de un host.
type HostEvent struct {
	ID         int64
	FromStatus string
	ToStatus   string
	At         time.Time
	Source     string
}

// AlertDetail es una alerta con sus entregas y los ultimos eventos de su host.
type AlertDetail struct {
	Alert      Alert
	Deliveries []Delivery
	HostEvents []HostEvent
}

// SeverityCount cuenta las alertas activas de una severidad por estado.
type SeverityCount struct {
	Open         int
	Acknowledged int
}

// AlertSummary son los contadores de alertas activas (no resueltas).
type AlertSummary struct {
	Active     int
	Suppressed int
	// BySeverity siempre trae info, warn y critical.
	BySeverity map[string]SeverityCount
}

const alertSelect = `
SELECT a.id, a.rule_id, a.rule_name, a.rule_kind, a.metric, a.threshold, a.severity, a.state, a.opened_at,
       a.trigger_value, a.peak_value, a.suppressed, a.host_id, h.hostname,
       a.acknowledged_by_name, a.acknowledged_at, a.resolved_at, a.resolved_by_name, a.resolve_reason
  FROM alerts a JOIN hosts h ON h.id = a.host_id`

func scanAlert(row pgx.Row) (Alert, error) {
	var a Alert
	err := row.Scan(&a.ID, &a.RuleID, &a.RuleName, &a.RuleKind, &a.Metric, &a.Threshold, &a.Severity, &a.State, &a.OpenedAt,
		&a.TriggerValue, &a.PeakValue, &a.Suppressed, &a.HostID, &a.Hostname,
		&a.AcknowledgedByName, &a.AcknowledgedAt, &a.ResolvedAt, &a.ResolvedByName, &a.ResolveReason)
	return a, err
}

// alertWhere arma el WHERE de los filtros con parametros posicionales (solo texto fijo; los valores van como
// argumentos). Cada filtro presente agrega una condicion: sin OR-NULL, pa que el planificador use los indices.
func alertWhere(f AlertFilter) (conds []string, args []any) {
	add := func(cond string, v any) {
		args = append(args, v)
		conds = append(conds, strings.ReplaceAll(cond, "$$", "$"+strconv.Itoa(len(args))))
	}
	if len(f.States) > 0 {
		add("a.state = ANY($$)", f.States)
	}
	if len(f.Severities) > 0 {
		add("a.severity = ANY($$)", f.Severities)
	}
	if len(f.HostIDs) > 0 {
		add("a.host_id = ANY($$::uuid[])", f.HostIDs)
	}
	if len(f.RuleIDs) > 0 {
		add("a.rule_id = ANY($$::uuid[])", f.RuleIDs)
	}
	return conds, args
}

// ListAlerts devuelve una pagina de alertas, las mas recientes primero (keyset por (opened_at, id)).
func (s *Store) ListAlerts(ctx context.Context, f AlertFilter) (AlertPage, error) {
	if f.Limit < 1 || f.Limit > MaxAlertListLimit {
		f.Limit = DefaultAlertListLimit
	}
	conds, args := alertWhere(f)

	var page AlertPage
	countSQL := `SELECT count(*)::int FROM alerts a`
	if len(conds) > 0 {
		countSQL += ` WHERE ` + strings.Join(conds, ` AND `)
	}
	if err := s.pool.QueryRow(ctx, countSQL, args...).Scan(&page.Total); err != nil {
		return AlertPage{}, fmt.Errorf("contando alertas: %w", err)
	}

	if f.After != nil {
		args = append(args, f.After.OpenedAt, f.After.ID)
		conds = append(conds, fmt.Sprintf("(a.opened_at, a.id) < ($%d, $%d::uuid)", len(args)-1, len(args)))
	}
	args = append(args, f.Limit+1)
	sql := alertSelect
	if len(conds) > 0 {
		sql += ` WHERE ` + strings.Join(conds, ` AND `)
	}
	sql += fmt.Sprintf(` ORDER BY a.opened_at DESC, a.id DESC LIMIT $%d`, len(args))

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return AlertPage{}, fmt.Errorf("listando alertas: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Alert, error) { return scanAlert(row) })
	if err != nil {
		return AlertPage{}, fmt.Errorf("leyendo alertas: %w", err)
	}
	if len(items) > f.Limit {
		items = items[:f.Limit]
		last := items[len(items)-1]
		page.NextCursor = &AlertCursor{OpenedAt: last.OpenedAt, ID: last.ID}
	}
	page.Items = items
	return page, nil
}

// AlertByID devuelve una alerta con sus entregas y los ultimos eventos de su host. ErrNotFound si no existe.
func (s *Store) AlertByID(ctx context.Context, id string) (AlertDetail, error) {
	a, err := scanAlert(s.pool.QueryRow(ctx, alertSelect+` WHERE a.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return AlertDetail{}, ErrNotFound
	}
	if err != nil {
		return AlertDetail{}, fmt.Errorf("leyendo alerta: %w", err)
	}
	d := AlertDetail{Alert: a}

	rows, err := s.pool.Query(ctx,
		`SELECT d.id, d.channel_id, c.name, d.event, d.seq, d.status, d.attempts, d.created_at, d.sent_at
		   FROM notification_deliveries d JOIN notification_channels c ON c.id = d.channel_id
		  WHERE d.alert_id = $1 ORDER BY d.created_at, d.id LIMIT 100`, id)
	if err != nil {
		return AlertDetail{}, fmt.Errorf("listando entregas: %w", err)
	}
	d.Deliveries, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Delivery, error) {
		var x Delivery
		err := row.Scan(&x.ID, &x.ChannelID, &x.ChannelName, &x.Event, &x.Seq, &x.Status, &x.Attempts, &x.CreatedAt, &x.SentAt)
		return x, err
	})
	if err != nil {
		return AlertDetail{}, fmt.Errorf("leyendo entregas: %w", err)
	}

	page, err := s.ListHostEvents(ctx, a.HostID, nil, alertDetailEvents)
	if err != nil {
		return AlertDetail{}, err
	}
	d.HostEvents = page.Items
	return d, nil
}

// ErrAlertResolved: la alerta ya esta resuelta y no admite la accion pedida.
var ErrAlertResolved = errors.New("la alerta ya esta resuelta")

// AckAlert reconoce una alerta abierta. Es idempotente sobre una ya reconocida. ErrNotFound si no existe;
// ErrAlertResolved si ya esta resuelta.
func (s *Store) AckAlert(ctx context.Context, id, userID, username string) (Alert, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE alerts SET state = 'acknowledged', acknowledged_by = $2::uuid, acknowledged_by_name = $3, acknowledged_at = now()
		  WHERE id = $1 AND state = 'open'`, id, userID, username)
	if err != nil {
		return Alert{}, fmt.Errorf("reconociendo alerta: %w", err)
	}
	a, err := s.alertByID(ctx, id)
	if err != nil {
		return Alert{}, err
	}
	if tag.RowsAffected() == 0 && a.State == "resolved" {
		return Alert{}, ErrAlertResolved
	}
	return a, nil
}

// ResolveAlert resuelve a mano una alerta activa y encola el aviso `resolved` a los canales que recibieron el
// `opened`. Si la condicion sigue incumpliendose, el evaluador abrira otra alerta tras la duracion de la regla.
// ErrNotFound si no existe; ErrAlertResolved si ya estaba resuelta.
func (s *Store) ResolveAlert(ctx context.Context, id, userID, username string) (Alert, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
WITH res AS (
  UPDATE alerts SET state = 'resolved', resolved_at = now(), resolved_by = $2::uuid, resolved_by_name = $3,
                    resolve_reason = 'manual', clear_since = NULL
   WHERE id = $1 AND state <> 'resolved'
  RETURNING id
)`+enqueueResolvedCTE+` SELECT count(*)::int FROM res`, id, userID, username).Scan(&n)
	if err != nil {
		return Alert{}, fmt.Errorf("resolviendo alerta: %w", err)
	}
	a, err := s.alertByID(ctx, id)
	if err != nil {
		return Alert{}, err
	}
	if n == 0 {
		return Alert{}, ErrAlertResolved
	}
	return a, nil
}

func (s *Store) alertByID(ctx context.Context, id string) (Alert, error) {
	a, err := scanAlert(s.pool.QueryRow(ctx, alertSelect+` WHERE a.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Alert{}, ErrNotFound
	}
	if err != nil {
		return Alert{}, fmt.Errorf("leyendo alerta: %w", err)
	}
	return a, nil
}

// AlertSummary cuenta las alertas activas por severidad y estado.
func (s *Store) AlertSummary(ctx context.Context) (AlertSummary, error) {
	sum := AlertSummary{BySeverity: map[string]SeverityCount{"info": {}, "warn": {}, "critical": {}}}
	rows, err := s.pool.Query(ctx,
		`SELECT severity, state, count(*)::int, count(*) FILTER (WHERE suppressed)::int
		   FROM alerts WHERE state <> 'resolved' GROUP BY severity, state`)
	if err != nil {
		return AlertSummary{}, fmt.Errorf("resumiendo alertas: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var sev, state string
		var n, suppressed int
		if err := rows.Scan(&sev, &state, &n, &suppressed); err != nil {
			return AlertSummary{}, fmt.Errorf("leyendo resumen de alertas: %w", err)
		}
		c := sum.BySeverity[sev]
		if state == "acknowledged" {
			c.Acknowledged += n
		} else {
			c.Open += n
		}
		sum.BySeverity[sev] = c
		sum.Active += n
		sum.Suppressed += suppressed
	}
	if err := rows.Err(); err != nil {
		return AlertSummary{}, fmt.Errorf("iterando resumen de alertas: %w", err)
	}
	return sum, nil
}

// EventCursor es la posicion keyset (at, id) tras el ultimo evento de una pagina.
type EventCursor struct {
	At time.Time
	ID int64
}

// EventPage es una pagina de eventos de un host, los mas recientes primero.
type EventPage struct {
	Items      []HostEvent
	NextCursor *EventCursor
}

// ListHostEvents devuelve las transiciones de status del host (mas recientes primero). ErrNotFound si el host
// no existe. limit debe estar entre 1 y MaxAlertListLimit.
func (s *Store) ListHostEvents(ctx context.Context, hostID string, after *EventCursor, limit int) (EventPage, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM hosts WHERE id = $1)`, hostID).Scan(&exists); err != nil {
		return EventPage{}, fmt.Errorf("buscando host: %w", err)
	}
	if !exists {
		return EventPage{}, ErrNotFound
	}
	sql := `SELECT id, from_status, to_status, at, source FROM host_events WHERE host_id = $1`
	args := []any{hostID}
	if after != nil {
		args = append(args, after.At, after.ID)
		sql += ` AND (at, id) < ($2, $3)`
	}
	args = append(args, limit+1)
	sql += fmt.Sprintf(` ORDER BY at DESC, id DESC LIMIT $%d`, len(args))

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return EventPage{}, fmt.Errorf("listando eventos del host: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (HostEvent, error) {
		var e HostEvent
		err := row.Scan(&e.ID, &e.FromStatus, &e.ToStatus, &e.At, &e.Source)
		return e, err
	})
	if err != nil {
		return EventPage{}, fmt.Errorf("leyendo eventos del host: %w", err)
	}
	var page EventPage
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		page.NextCursor = &EventCursor{At: last.At, ID: last.ID}
	}
	page.Items = items
	return page, nil
}

// Retencion: cada Purge* borra hasta batch filas anteriores a before y devuelve cuantas borro.

// PurgeHostEvents borra eventos de host anteriores a before.
func (s *Store) PurgeHostEvents(ctx context.Context, before time.Time, batch int) (int, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM host_events WHERE id IN (SELECT id FROM host_events WHERE at < $1 ORDER BY at LIMIT $2)`, before, batch)
	if err != nil {
		return 0, fmt.Errorf("purgando eventos de host: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// PurgeResolvedAlerts borra alertas resueltas antes de before (sus entregas caen en cascada).
func (s *Store) PurgeResolvedAlerts(ctx context.Context, before time.Time, batch int) (int, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM alerts WHERE id IN (
		   SELECT id FROM alerts WHERE state = 'resolved' AND resolved_at < $1 ORDER BY resolved_at LIMIT $2)`, before, batch)
	if err != nil {
		return 0, fmt.Errorf("purgando alertas resueltas: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// PurgeDeliveries borra entregas terminadas (sent, dead, suppressed) creadas antes de before. Las pending y
// sending no se tocan: son trabajo vivo del worker.
func (s *Store) PurgeDeliveries(ctx context.Context, before time.Time, batch int) (int, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM notification_deliveries WHERE id IN (
		   SELECT id FROM notification_deliveries WHERE created_at < $1 AND status IN ('sent', 'dead', 'suppressed')
		    ORDER BY created_at LIMIT $2)`, before, batch)
	if err != nil {
		return 0, fmt.Errorf("purgando entregas: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// PurgeMaintenanceWindows borra ventanas de mantenimiento terminadas antes de before.
func (s *Store) PurgeMaintenanceWindows(ctx context.Context, before time.Time, batch int) (int, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM maintenance_windows WHERE id IN (SELECT id FROM maintenance_windows WHERE ends_at < $1 ORDER BY ends_at LIMIT $2)`,
		before, batch)
	if err != nil {
		return 0, fmt.Errorf("purgando ventanas de mantenimiento: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
