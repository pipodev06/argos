package store

import (
	"context"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
)

const (
	// alertEvalLockName da la clave (hashtext) del lock advisory de transaccion que serializa el evaluador entre
	// instancias: la que no lo obtiene salta el ciclo.
	alertEvalLockName = "argos.alert_eval"

	// alertEvalTimeout acota cada sentencia del ciclo (SET LOCAL statement_timeout, en ms).
	alertEvalTimeoutMS = 20000

	// MaxPendingDeliveries es el tope de entregas `pending`: por encima, las nuevas se encolan `suppressed`
	// (evita que una tormenta de alertas llene la cola). Es un tope blando: se comprueba por sentencia.
	MaxPendingDeliveries = 10000

	// ReminderInterval es cada cuanto se recuerda una alerta critica sin reconocer.
	ReminderInterval = 4 * 3600 // segundos

	// FlapWindow y FlapThreshold definen una alerta intermitente: la apertura n.° FlapThreshold (contando la actual)
	// de la misma regla+host dentro de FlapWindow se marca flapping y no notifica.
	FlapWindow    = 30 * 60 // segundos
	FlapThreshold = 3
)

// channelOwnsHostSQL es verdadero si el canal `ch` puede recibir avisos del host indicado: un canal global (sin
// dueño) recibe todo; uno con dueño solo los de hosts de su grupo. Defensa en profundidad: aunque haya datos previos
// incoherentes (regla fuera del dueño), nunca se encola un aviso con datos de otro grupo.
func channelOwnsHostSQL(hostCol string) string {
	return `(ch.group_id IS NULL OR ch.group_id = (SELECT hg.group_id FROM hosts hg WHERE hg.id = ` + hostCol + `))`
}

// pendingCapSQL es verdadero cuando la cola ya llego al tope. El LIMIT acota el conteo.
var pendingCapSQL = `(SELECT count(*) FROM (SELECT 1 FROM notification_deliveries WHERE status = 'pending' LIMIT ` +
	strconv.Itoa(MaxPendingDeliveries) + `) cap_q) >= ` + strconv.Itoa(MaxPendingDeliveries)

// severityRankSQL compara severidades (info < warn < critical) sin depender del orden alfabetico.
const severityRankSQL = `array_position(ARRAY['info', 'warn', 'critical'], %s)`

// enqueueResolvedCTE encola el aviso `resolved` de cada alerta del CTE `res` (columnas id y host_id) a los canales que
// recibieron (o recibiran) su `opened` o su `escalation`: no se anuncia la resolucion de algo que nunca se anuncio.
var enqueueResolvedCTE = `, enq AS (
  INSERT INTO notification_deliveries (alert_id, channel_id, event, status)
  SELECT res.id, d.channel_id, 'resolved', CASE WHEN ` + pendingCapSQL + ` THEN 'suppressed' ELSE 'pending' END
    FROM res
    JOIN notification_deliveries d ON d.alert_id = res.id AND d.event IN ('opened', 'escalation') AND d.status IN ('pending', 'sending', 'sent')
    JOIN notification_channels ch ON ch.id = d.channel_id AND ch.enabled
     AND ` + channelOwnsHostSQL("res.host_id") + `
  ON CONFLICT DO NOTHING
)`

// Pasos del ciclo de evaluacion, en orden. Todos leen la vista alert_candidates (migracion 00015) y usan now()
// de la BD, que dentro de la transaccion es constante: todo el ciclo ve el mismo instante.
var (
	// 1. Alertas cuya regla se borro o desactivo, o cuyo host se deshabilito: se resuelven.
	resolveOrphansSQL = `
WITH res AS (
  UPDATE alerts a
     SET state = 'resolved', resolved_at = now(), clear_since = NULL,
         resolve_reason = CASE WHEN h.status = 'disabled' THEN 'host_disabled'
                               WHEN a.rule_id IS NULL THEN 'rule_deleted' ELSE 'rule_disabled' END
    FROM hosts h
   WHERE h.id = a.host_id AND a.state <> 'resolved'
     AND (h.status = 'disabled' OR a.rule_id IS NULL
          OR EXISTS (SELECT 1 FROM alert_rules r WHERE r.id = a.rule_id AND NOT r.enabled))
  RETURNING a.id, a.host_id
)` + enqueueResolvedCTE + ` SELECT count(*) FROM res`

	// 2. Pendientes que ya no incumplen (o ya no son evaluables: host offline, regla desactivada) o que ya
	// tienen alerta activa.
	dropPendingSQL = `
DELETE FROM alert_pending p
 WHERE NOT EXISTS (SELECT 1 FROM alert_candidates c WHERE c.rule_id = p.rule_id AND c.host_id = p.host_id AND c.breach)
    OR EXISTS (SELECT 1 FROM alerts a WHERE a.rule_id = p.rule_id AND a.host_id = p.host_id AND a.state <> 'resolved')`

	// 3. Incumplimientos nuevos. `since` no se reinicia (ON CONFLICT DO NOTHING); host_status y no_data traen su
	// inicio real de los datos del host.
	startPendingSQL = `
INSERT INTO alert_pending (rule_id, host_id, since)
SELECT c.rule_id, c.host_id, LEAST(now(), COALESCE(c.breach_since, now()))
  FROM alert_candidates c
 WHERE c.breach
   AND NOT EXISTS (SELECT 1 FROM alerts a WHERE a.rule_id = c.rule_id AND a.host_id = c.host_id AND a.state <> 'resolved')
ON CONFLICT DO NOTHING`

	// 4. Abrir alertas cuyo incumplimiento ya duro duration_seconds. El indice unico parcial es el dedupe duro.
	// Dentro de una ventana de mantenimiento, o si es intermitente (flapping), la alerta se abre igual (visible) con
	// sus entregas `suppressed`.
	openAlertsSQL = `
WITH new AS (
  INSERT INTO alerts (rule_id, host_id, rule_name, rule_kind, metric, threshold, severity, trigger_value, peak_value, suppressed, detail, flapping)
  SELECT r.id, c.host_id, r.name, r.kind, r.metric, r.threshold,
         CASE WHEN r.kind = 'os_eol' AND c.value <= 0 THEN 'critical' ELSE r.severity END,
         c.value, c.value, c.in_maintenance, c.detail,
         (SELECT count(*) FROM alerts pa WHERE pa.rule_id = r.id AND pa.host_id = c.host_id
             AND pa.opened_at > now() - make_interval(secs => ` + strconv.Itoa(FlapWindow) + `)) >= ` + strconv.Itoa(FlapThreshold-1) + `
    FROM alert_pending p
    JOIN alert_candidates c ON c.rule_id = p.rule_id AND c.host_id = p.host_id AND c.breach
    JOIN alert_rules r ON r.id = p.rule_id
   WHERE p.since <= now() - make_interval(secs => r.duration_seconds)
  ON CONFLICT (rule_id, host_id) WHERE state <> 'resolved' DO NOTHING
  RETURNING id, rule_id, host_id, severity, suppressed, flapping
), del AS (
  DELETE FROM alert_pending p USING new WHERE p.rule_id = new.rule_id AND p.host_id = new.host_id
), enq AS (
  INSERT INTO notification_deliveries (alert_id, channel_id, event, status)
  SELECT new.id, ch.id, 'opened',
         CASE WHEN new.suppressed OR new.flapping OR (r.muted_until IS NOT NULL AND r.muted_until > now()) OR ` + pendingCapSQL + `
              THEN 'suppressed' ELSE 'pending' END
    FROM new
    JOIN alert_rules r ON r.id = new.rule_id
    JOIN rule_channels rc ON rc.rule_id = new.rule_id
    JOIN notification_channels ch ON ch.id = rc.channel_id AND ch.enabled
     AND ` + channelOwnsHostSQL("new.host_id") + `
     AND ` + fmt.Sprintf(severityRankSQL, "new.severity") + ` >= ` + fmt.Sprintf(severityRankSQL, "ch.min_severity") + `
  ON CONFLICT DO NOTHING
)
SELECT count(*) FROM new`

	// 5. Alertas activas: pico, severidad y desde cuando el valor esta del lado sano. Solo escribe si algo cambio.
	// Pico = el peor valor visto: el minimo si la regla es "menor que" y en os_eol (dias que faltan; op es NULL), el
	// maximo en el resto. Una alerta os_eol abierta con la regla en warn/info sube a critica cuando el SO vence (nunca
	// baja); es un cambio silencioso: no se reenvia la notificacion.
	touchAlertsSQL = `
UPDATE alerts a SET peak_value = u.peak, clear_since = u.clr, detail = u.dt, severity = u.sev
  FROM (SELECT a2.id,
               CASE WHEN c.value IS NULL THEN a2.peak_value
                    WHEN a2.peak_value IS NULL THEN c.value
                    WHEN r.op = 'lt' OR r.kind = 'os_eol' THEN LEAST(a2.peak_value, c.value)
                    ELSE GREATEST(a2.peak_value, c.value) END AS peak,
               COALESCE(c.detail, a2.detail) AS dt,
               CASE WHEN r.kind = 'os_eol' AND c.value <= 0 THEN 'critical' ELSE a2.severity END AS sev,
               CASE WHEN c.clear THEN COALESCE(a2.clear_since, now()) END AS clr
          FROM alerts a2
          JOIN alert_candidates c ON c.rule_id = a2.rule_id AND c.host_id = a2.host_id
          JOIN alert_rules r ON r.id = a2.rule_id
         WHERE a2.state <> 'resolved') u
 WHERE a.id = u.id AND (a.peak_value IS DISTINCT FROM u.peak OR a.clear_since IS DISTINCT FROM u.clr OR a.detail IS DISTINCT FROM u.dt
        OR a.severity IS DISTINCT FROM u.sev)`

	// 6. Resolver las que llevan resolve_seconds del lado sano.
	resolveRecoveredSQL = `
WITH res AS (
  UPDATE alerts a SET state = 'resolved', resolved_at = now(), resolve_reason = 'recovered', clear_since = NULL
    FROM alert_rules r
   WHERE r.id = a.rule_id AND a.state <> 'resolved' AND a.clear_since IS NOT NULL
     AND a.clear_since <= now() - make_interval(secs => r.resolve_seconds)
  RETURNING a.id, a.host_id
)` + enqueueResolvedCTE + ` SELECT count(*) FROM res`

	// 7. Alertas abiertas en mantenimiento cuya ventana ya termino (y el host sigue evaluable): se liberan y su
	// `opened` retenido pasa a pending. Si ya estaban reconocidas solo se limpia la marca.
	releaseSuppressedSQL = `
WITH rel AS (
  UPDATE alerts a SET suppressed = false
    FROM alert_candidates c
   WHERE a.suppressed AND a.state <> 'resolved' AND c.rule_id = a.rule_id AND c.host_id = a.host_id AND NOT c.in_maintenance
  RETURNING a.id, a.state
), up AS (
  UPDATE notification_deliveries d SET status = 'pending', next_attempt_at = now()
    FROM rel
   WHERE rel.state = 'open' AND d.alert_id = rel.id AND d.event = 'opened' AND d.status = 'suppressed'
)
SELECT count(*) FROM rel`

	// 8. Recordatorio cada 4 h de las criticas sin reconocer (solo a los canales que recibieron el `opened` y solo
	// si el host es evaluable: un host offline no notifica). seq = numero de recordatorio: la UNIQUE lo hace idempotente.
	remindersSQL = `
WITH ins AS (
INSERT INTO notification_deliveries (alert_id, channel_id, event, seq, status)
SELECT a.id, d.channel_id, 'reminder', k.n,
       CASE WHEN c.in_maintenance OR (r.muted_until IS NOT NULL AND r.muted_until > now()) OR ` + pendingCapSQL + `
            THEN 'suppressed' ELSE 'pending' END
  FROM alerts a
  JOIN alert_rules r ON r.id = a.rule_id
  JOIN alert_candidates c ON c.rule_id = a.rule_id AND c.host_id = a.host_id AND NOT c.clear
 CROSS JOIN LATERAL (SELECT floor(extract(epoch FROM now() - a.opened_at) / ` + strconv.Itoa(ReminderInterval) + `)::int AS n) k
  JOIN notification_deliveries d ON d.alert_id = a.id AND d.event = 'opened' AND d.status IN ('pending', 'sending', 'sent')
  JOIN notification_channels ch ON ch.id = d.channel_id AND ch.enabled AND ` + channelOwnsHostSQL("a.host_id") + `
 WHERE a.state = 'open' AND a.severity = 'critical' AND NOT a.suppressed AND k.n >= 1
ON CONFLICT DO NOTHING
RETURNING 1
)
SELECT count(*) FROM ins`

	// 9. Escalamiento: una alerta `open` (sin reconocer) que lleva escalate_after_seconds de la regla sube un nivel de
	// severidad (critical se queda) y avisa una sola vez (escalated_at) a los canales de escalamiento de la regla. No
	// escala una alerta en mantenimiento ni intermitente. La severidad ya subida es la que filtra min_severity.
	escalateSQL = `
WITH esc AS (
  UPDATE alerts a
     SET escalated_at = now(),
         severity = CASE a.severity WHEN 'info' THEN 'warn' WHEN 'warn' THEN 'critical' ELSE a.severity END
    FROM alert_rules r
   WHERE r.id = a.rule_id AND r.escalate_after_seconds IS NOT NULL AND a.state = 'open' AND a.escalated_at IS NULL
     AND NOT a.suppressed AND NOT a.flapping
     AND a.opened_at <= now() - make_interval(secs => r.escalate_after_seconds)
  RETURNING a.id, a.rule_id, a.host_id, a.severity
), enq AS (
  INSERT INTO notification_deliveries (alert_id, channel_id, event, status)
  SELECT esc.id, ch.id, 'escalation',
         CASE WHEN (r.muted_until IS NOT NULL AND r.muted_until > now()) OR ` + pendingCapSQL + `
              THEN 'suppressed' ELSE 'pending' END
    FROM esc
    JOIN alert_rules r ON r.id = esc.rule_id
    JOIN rule_escalation_channels rc ON rc.rule_id = esc.rule_id
    JOIN notification_channels ch ON ch.id = rc.channel_id AND ch.enabled
     AND ` + channelOwnsHostSQL("esc.host_id") + `
     AND ` + fmt.Sprintf(severityRankSQL, "esc.severity") + ` >= ` + fmt.Sprintf(severityRankSQL, "ch.min_severity") + `
  ON CONFLICT DO NOTHING
)
SELECT count(*) FROM esc`
)

// EvalResult resume un ciclo del evaluador.
type EvalResult struct {
	// Ran es false si otra instancia tenia el lock (ciclo saltado).
	Ran bool
	// Opened, Resolved (por recuperacion o por regla/host), Released (liberadas de mantenimiento) y Reminders
	// (entregas de recordatorio encoladas) cuentan lo que cambio en el ciclo; Escalated son las alertas que subieron de severidad.
	Opened, Resolved, Released, Reminders, Escalated int
}

// EvaluateAlerts corre un ciclo del motor de alertas en UNA transaccion bajo un lock advisory (con varias
// instancias solo una evalua; la otra devuelve Ran=false). Lee solo hosts, host_latest y etiquetas.
func (s *Store) EvaluateAlerts(ctx context.Context) (EvalResult, error) {
	var res EvalResult
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var locked bool
		if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtext($1))`, alertEvalLockName).Scan(&locked); err != nil {
			return fmt.Errorf("lock del evaluador: %w", err)
		}
		if !locked {
			return nil
		}
		res.Ran = true
		// statement_timeout no admite parametros; el valor es una constante.
		if _, err := tx.Exec(ctx, fmt.Sprintf(`SET LOCAL statement_timeout = %d`, alertEvalTimeoutMS)); err != nil {
			return fmt.Errorf("fijando statement_timeout: %w", err)
		}

		steps := []struct {
			name string
			sql  string
			add  *int // nil = la sentencia no devuelve conteo (Exec)
		}{
			{"resolviendo alertas huerfanas", resolveOrphansSQL, &res.Resolved},
			{"limpiando pendientes", dropPendingSQL, nil},
			{"registrando incumplimientos", startPendingSQL, nil},
			{"abriendo alertas", openAlertsSQL, &res.Opened},
			{"actualizando alertas activas", touchAlertsSQL, nil},
			{"resolviendo alertas recuperadas", resolveRecoveredSQL, &res.Resolved},
			{"liberando alertas de mantenimiento", releaseSuppressedSQL, &res.Released},
			{"encolando recordatorios", remindersSQL, &res.Reminders},
			{"escalando alertas sin reconocer", escalateSQL, &res.Escalated},
		}
		for _, st := range steps {
			if st.add == nil {
				if _, err := tx.Exec(ctx, st.sql); err != nil {
					return fmt.Errorf("%s: %w", st.name, err)
				}
				continue
			}
			var n int
			if err := tx.QueryRow(ctx, st.sql).Scan(&n); err != nil {
				return fmt.Errorf("%s: %w", st.name, err)
			}
			*st.add += n
		}
		return nil
	})
	if err != nil {
		return EvalResult{}, fmt.Errorf("evaluando alertas: %w", err)
	}
	return res, nil
}
