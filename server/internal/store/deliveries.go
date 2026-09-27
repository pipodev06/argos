package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Politica de la cola de entregas (decidida en el diseno de alertas).
const (
	// MaxDeliveryAttempts: tras tantos intentos fallidos la entrega queda `dead`.
	MaxDeliveryAttempts = 6
	// MaxDeliveryAge: una entrega mas vieja que esto no se reintenta mas (`dead`).
	MaxDeliveryAge = 24 * time.Hour
	// ChannelHourlyLimit: mensajes por canal en la ultima hora; el exceso espera y se colapsa en un resumen.
	ChannelHourlyLimit = 30
)

// DeliveryJob es una entrega reclamada por el worker, con lo necesario pa armar y enviar el mensaje.
type DeliveryJob struct {
	ID        int64
	AlertID   string
	ChannelID string
	Event     string
	Seq       int
	// Attempts incluye el intento en curso (el reclamo lo incrementa).
	Attempts  int
	CreatedAt time.Time

	Hostname     string
	RuleName     string
	RuleKind     string
	Severity     string
	TriggerValue *float64
	Threshold    *float64
	Detail       *string

	ChannelName    string
	ChannelKind    string
	ChannelEnabled bool
	SecretEnc      []byte
	SecretKeyID    *string
}

// claimDeliveriesSQL toma hasta $1 entregas vencidas con FOR UPDATE SKIP LOCKED (varias instancias no se pisan)
// y les pone un lease de $3 s. warn/info esperan $2 s desde su creacion (pa agruparse); critical va de inmediato.
// Tambien recupera las `sending` cuyo lease vencio (un worker que murio). Todo con now() de la BD.
const claimDeliveriesSQL = `
WITH due AS (
  SELECT d.id
    FROM notification_deliveries d JOIN alerts a ON a.id = d.alert_id
   WHERE ((d.status = 'pending' AND d.next_attempt_at <= now()) OR (d.status = 'sending' AND d.lease_until <= now()))
     AND (a.severity = 'critical' OR d.created_at <= now() - make_interval(secs => $2))
   ORDER BY d.next_attempt_at, d.id
   LIMIT $1
   FOR UPDATE OF d SKIP LOCKED
), up AS (
  UPDATE notification_deliveries d
     SET status = 'sending', attempts = d.attempts + 1, lease_until = now() + make_interval(secs => $3)
    FROM due WHERE d.id = due.id
  RETURNING d.id, d.alert_id, d.channel_id, d.event, d.seq, d.attempts, d.created_at
)
SELECT up.id, up.alert_id, up.channel_id, up.event, up.seq, up.attempts, up.created_at,
       h.hostname, a.rule_name, a.rule_kind, a.severity, a.trigger_value, a.threshold, a.detail,
       c.name, c.kind, c.enabled, c.secret_enc, c.secret_key_id
  FROM up
  JOIN alerts a ON a.id = up.alert_id
  JOIN hosts h ON h.id = a.host_id
  JOIN notification_channels c ON c.id = up.channel_id
 ORDER BY up.channel_id, up.id`

// ClaimDeliveries reclama hasta limit entregas listas pa enviar.
func (s *Store) ClaimDeliveries(ctx context.Context, limit int, warnDelay, lease time.Duration) ([]DeliveryJob, error) {
	rows, err := s.pool.Query(ctx, claimDeliveriesSQL, limit, warnDelay.Seconds(), lease.Seconds())
	if err != nil {
		return nil, fmt.Errorf("reclamando entregas: %w", err)
	}
	jobs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (DeliveryJob, error) {
		var j DeliveryJob
		err := row.Scan(&j.ID, &j.AlertID, &j.ChannelID, &j.Event, &j.Seq, &j.Attempts, &j.CreatedAt,
			&j.Hostname, &j.RuleName, &j.RuleKind, &j.Severity, &j.TriggerValue, &j.Threshold, &j.Detail,
			&j.ChannelName, &j.ChannelKind, &j.ChannelEnabled, &j.SecretEnc, &j.SecretKeyID)
		return j, err
	})
	if err != nil {
		return nil, fmt.Errorf("leyendo entregas reclamadas: %w", err)
	}
	return jobs, nil
}

// MarkDeliveriesSent marca como enviadas (un mismo mensaje agrupado comparte sent_at).
func (s *Store) MarkDeliveriesSent(ctx context.Context, ids []int64, httpStatus int) error {
	if _, err := s.pool.Exec(ctx,
		`UPDATE notification_deliveries
		    SET status = 'sent', sent_at = now(), lease_until = NULL, last_error = NULL, http_status = $2
		  WHERE id = ANY($1) AND status = 'sending'`, ids, httpStatus); err != nil {
		return fmt.Errorf("marcando entregas enviadas: %w", err)
	}
	return nil
}

// DeliveryFailure describe el fallo de un lote de entregas. Delays[i] es la espera antes de reintentar IDs[i].
type DeliveryFailure struct {
	IDs        []int64
	Delays     []time.Duration
	LastError  string // ya saneado, <= 300 caracteres
	HTTPStatus *int
}

// MarkDeliveriesFailed devuelve las entregas a pending con su backoff, o las deja `dead` si ya usaron
// MaxDeliveryAttempts intentos o superan MaxDeliveryAge.
func (s *Store) MarkDeliveriesFailed(ctx context.Context, f DeliveryFailure) error {
	secs := make([]float64, len(f.Delays))
	for i, d := range f.Delays {
		secs[i] = d.Seconds()
	}
	if _, err := s.pool.Exec(ctx,
		`UPDATE notification_deliveries d
		    SET status = CASE WHEN d.attempts >= $4 OR d.created_at <= now() - make_interval(secs => $5) THEN 'dead' ELSE 'pending' END,
		        dead_at = CASE WHEN d.attempts >= $4 OR d.created_at <= now() - make_interval(secs => $5) THEN now() END,
		        next_attempt_at = now() + make_interval(secs => v.delay), lease_until = NULL,
		        last_error = left($3, 300), http_status = $6
		   FROM unnest($1::bigint[], $2::float8[]) AS v(id, delay)
		  WHERE d.id = v.id AND d.status = 'sending'`,
		f.IDs, secs, f.LastError, MaxDeliveryAttempts, MaxDeliveryAge.Seconds(), f.HTTPStatus); err != nil {
		return fmt.Errorf("marcando entregas fallidas: %w", err)
	}
	return nil
}

// ReleaseDeliveries devuelve entregas reclamadas a pending sin gastar un intento (p. ej. el canal llego a su
// limite por hora). Pasadas MaxDeliveryAge quedan `dead`.
func (s *Store) ReleaseDeliveries(ctx context.Context, ids []int64, wait time.Duration, reason string) error {
	if _, err := s.pool.Exec(ctx,
		`UPDATE notification_deliveries
		    SET status = CASE WHEN created_at <= now() - make_interval(secs => $4) THEN 'dead' ELSE 'pending' END,
		        dead_at = CASE WHEN created_at <= now() - make_interval(secs => $4) THEN now() END,
		        attempts = attempts - 1, lease_until = NULL, next_attempt_at = now() + make_interval(secs => $2),
		        last_error = left($3, 300)
		  WHERE id = ANY($1) AND status = 'sending'`,
		ids, wait.Seconds(), reason, MaxDeliveryAge.Seconds()); err != nil {
		return fmt.Errorf("liberando entregas: %w", err)
	}
	return nil
}

// SuppressDeliveries marca entregas reclamadas como suppressed (canal deshabilitado o tipo sin soporte).
func (s *Store) SuppressDeliveries(ctx context.Context, ids []int64, reason string) error {
	if _, err := s.pool.Exec(ctx,
		`UPDATE notification_deliveries SET status = 'suppressed', lease_until = NULL, last_error = left($2, 300)
		  WHERE id = ANY($1) AND status = 'sending'`, ids, reason); err != nil {
		return fmt.Errorf("suprimiendo entregas: %w", err)
	}
	return nil
}

// ChannelHourUsage cuenta los mensajes enviados a un canal en la ultima hora (los de un mismo lote comparten
// sent_at) y dentro de cuanto sale el mas viejo de la ventana (0 si no hay ninguno).
func (s *Store) ChannelHourUsage(ctx context.Context, channelID string) (sent int, oldestLeavesIn time.Duration, err error) {
	var secs *float64
	err = s.pool.QueryRow(ctx,
		`SELECT count(DISTINCT sent_at)::int, extract(epoch FROM min(sent_at) + interval '1 hour' - now())::float8
		   FROM notification_deliveries
		  WHERE channel_id = $1 AND status = 'sent' AND sent_at > now() - interval '1 hour'`, channelID).Scan(&sent, &secs)
	if err != nil {
		return 0, 0, fmt.Errorf("contando mensajes del canal: %w", err)
	}
	if secs != nil && *secs > 0 {
		oldestLeavesIn = time.Duration(*secs * float64(time.Second))
	}
	return sent, oldestLeavesIn, nil
}
