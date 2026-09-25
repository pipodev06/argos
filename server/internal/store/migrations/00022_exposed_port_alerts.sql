-- +goose Up
-- Alerta "puerto nuevo expuesto": alert_rules.kind = 'exposed_port' con lista opcional expected_ports (puertos a
-- ignorar, max 100, [{proto, port}]) y alerts.detail (hasta 5 proto/puerto, texto pa notificaciones y detalle).
-- alert_candidates gana la columna detail. Sin motor nuevo: el evaluador de siempre lee la vista.

DROP VIEW alert_candidates;

-- Busqueda de "puerto anadido" por (host, clave) sin recorrer todos los cambios del host.
CREATE INDEX inventory_changes_ports_added_idx ON inventory_changes (host_id, key) WHERE module = 'ports' AND kind = 'added';

ALTER TABLE alert_rules ADD COLUMN expected_ports JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE alert_rules ADD CONSTRAINT alert_rules_expected_ports_check CHECK (
     jsonb_typeof(expected_ports) = 'array' AND jsonb_array_length(expected_ports) <= 100
 AND (kind = 'exposed_port' OR expected_ports = '[]'::jsonb)
 AND jsonb_array_length(jsonb_path_query_array(expected_ports,
       '$[*] ? ((@.proto == "tcp" || @.proto == "udp") && @.port.type() == "number" && @.port >= 1 && @.port <= 65535)'))
     = jsonb_array_length(expected_ports));

ALTER TABLE alert_rules DROP CONSTRAINT alert_rules_kind_check;
ALTER TABLE alert_rules DROP CONSTRAINT alert_rules_shape_check;
ALTER TABLE alert_rules ADD CONSTRAINT alert_rules_kind_check CHECK (kind IN ('metric', 'host_status', 'no_data', 'service_check', 'exposed_port'));
ALTER TABLE alert_rules ADD CONSTRAINT alert_rules_shape_check CHECK (
     (check_id IS NULL AND NOT warn_is_breach AND (
       (kind = 'metric' AND metric IS NOT NULL AND op IS NOT NULL AND threshold IS NOT NULL
          AND ((metric IN ('load1', 'load5', 'load15') AND threshold BETWEEN 0 AND 1000000
                  AND (clear_threshold IS NULL OR clear_threshold BETWEEN 0 AND 1000000))
            OR (metric NOT IN ('load1', 'load5', 'load15') AND threshold BETWEEN 0 AND 100
                  AND (clear_threshold IS NULL OR clear_threshold BETWEEN 0 AND 100)))
          AND (clear_threshold IS NULL OR (op = 'gt' AND clear_threshold <= threshold) OR (op = 'lt' AND clear_threshold >= threshold)))
    OR (kind = 'host_status' AND metric IS NULL AND op IS NULL AND threshold IS NULL AND clear_threshold IS NULL)
    OR (kind = 'exposed_port' AND metric IS NULL AND op IS NULL AND threshold IS NULL AND clear_threshold IS NULL)
    OR (kind = 'no_data' AND metric IS NULL AND op IS NULL AND clear_threshold IS NULL AND threshold BETWEEN 60 AND 2592000)))
  OR (kind = 'service_check' AND check_id IS NOT NULL AND metric IS NULL AND op IS NULL AND threshold IS NULL AND clear_threshold IS NULL));

ALTER TABLE alerts DROP CONSTRAINT alerts_rule_kind_check;
ALTER TABLE alerts ADD CONSTRAINT alerts_rule_kind_check CHECK (rule_kind IN ('metric', 'host_status', 'no_data', 'service_check', 'exposed_port'));
ALTER TABLE alerts ADD COLUMN detail TEXT CHECK (char_length(detail) <= 200 AND detail !~ '[[:cntrl:]]');

CREATE VIEW alert_candidates AS
SELECT r.id AS rule_id, h.id AS host_id, x.value, x.breach, x.clear, x.breach_since,
       EXISTS (SELECT 1 FROM maintenance_windows m
                WHERE m.starts_at <= now() AND m.ends_at > now()
                  AND (m.scope_type = 'all'
                       OR (m.scope_type = 'group' AND m.scope_group_id = h.group_id))
       ) AS in_maintenance,
       ep.ports AS detail
  FROM alert_rules r
  JOIN hosts h ON h.status NOT IN ('disabled', 'pending')
              AND (r.scope_type = 'all'
                   OR (r.scope_type = 'group' AND r.scope_group_id = h.group_id))
  LEFT JOIN host_latest l ON l.host_id = h.id
  LEFT JOIN service_check_defs d ON d.id = r.check_id
  LEFT JOIN service_check_state s ON s.host_id = h.id AND s.check_id = r.check_id
  LEFT JOIN LATERAL (
    -- exposed_port: puertos EXPUESTOS (bind distinto de loopback; vacio o ilegible = expuesto) anadidos despues
    -- de la linea base (hay un cambio 'added' en inventory_changes) y fuera de expected_ports. Cuenta (proto,
    -- puerto) distintos; ports lista hasta 5. Con otro kind el filtro constante evita leer inventario.
    SELECT count(*)::int AS n,
           string_agg(q.proto || '/' || q.port, ', ' ORDER BY q.port, q.proto) FILTER (WHERE q.rn <= 5) AS ports
      FROM (SELECT dp.proto, dp.port, row_number() OVER (ORDER BY dp.port, dp.proto) AS rn
              FROM (SELECT DISTINCT p.proto, p.port
                      FROM inventory_current ic
                     CROSS JOIN LATERAL jsonb_to_recordset(ic.items) AS p(proto text, bind text, port int)
                     WHERE r.kind = 'exposed_port' AND ic.host_id = h.id AND ic.module = 'ports'
                       AND COALESCE(p.bind, '') !~ '^(127\.[0-9]+\.[0-9]+\.[0-9]+|::1|::ffff:127\.[0-9]+\.[0-9]+\.[0-9]+)$'
                       AND NOT jsonb_path_exists(r.expected_ports, '$[*] ? (@.proto == $p && @.port == $n)',
                                                 jsonb_build_object('p', p.proto, 'n', p.port))
                       AND EXISTS (SELECT 1 FROM inventory_changes ch
                                    WHERE ch.host_id = h.id AND ch.module = 'ports' AND ch.kind = 'added'
                                      AND ch.key = p.proto || '|' || COALESCE(p.bind, '') || '|' || p.port)) dp) q
  ) ep ON true
 CROSS JOIN LATERAL (
   SELECT v.value,
          CASE r.kind
            WHEN 'metric' THEN v.value IS NOT NULL
                 AND (CASE r.op WHEN 'gt' THEN v.value > r.threshold ELSE v.value < r.threshold END)
                 AND (r.metric <> 'battery_pct' OR l.battery_charging IS NOT TRUE)
            WHEN 'host_status' THEN h.status = 'down'
            WHEN 'exposed_port' THEN ep.n > 0
            WHEN 'service_check' THEN s.ts >= now() - make_interval(secs => 3 * d.interval_seconds)
                 AND (s.status = 'down' OR (r.warn_is_breach AND s.status = 'warn'))
            ELSE h.last_seen_at IS NOT NULL AND h.last_seen_at < now() - make_interval(secs => r.threshold)
          END AS breach,
          CASE r.kind
            WHEN 'metric' THEN v.value IS NOT NULL
                 AND ((r.metric = 'battery_pct' AND l.battery_charging IS TRUE)
                      OR (CASE r.op WHEN 'gt' THEN v.value <= COALESCE(r.clear_threshold, r.threshold)
                                    ELSE v.value >= COALESCE(r.clear_threshold, r.threshold) END))
            WHEN 'host_status' THEN h.status <> 'down'
            WHEN 'exposed_port' THEN ep.n = 0
            WHEN 'service_check' THEN s.ts >= now() - make_interval(secs => 3 * d.interval_seconds)
                 AND (s.status = 'up' OR (NOT r.warn_is_breach AND s.status = 'warn'))
            ELSE h.last_seen_at IS NOT NULL AND h.last_seen_at >= now() - make_interval(secs => r.threshold)
          END AS clear,
          CASE r.kind
            WHEN 'host_status' THEN h.status_changed_at
            WHEN 'service_check' THEN s.status_changed_at
            WHEN 'no_data' THEN h.last_seen_at + make_interval(secs => r.threshold)
          END AS breach_since
     FROM (SELECT (CASE r.kind
                     WHEN 'metric' THEN
                       CASE r.metric
                         WHEN 'cpu_pct' THEN l.cpu_pct::double precision
                         WHEN 'mem_pct' THEN CASE WHEN l.mem_total_mb > 0 THEN l.mem_used_mb * 100.0 / l.mem_total_mb END::double precision
                         WHEN 'disk_max_pct' THEN CASE WHEN r.include_offline OR l.disk_ts > now() - interval '3 hours' THEN l.disk_max_pct END::double precision
                         WHEN 'battery_pct' THEN l.battery_pct::double precision
                         WHEN 'load1' THEN l.load1::double precision
                         WHEN 'load5' THEN l.load5::double precision
                         WHEN 'load15' THEN l.load15::double precision
                       END
                     WHEN 'exposed_port' THEN ep.n::double precision
                     WHEN 'no_data' THEN extract(epoch FROM now() - h.last_seen_at)::double precision
                     WHEN 'service_check' THEN COALESCE(s.value, s.response_ms)::double precision
                   END) AS value) v
 ) x
 WHERE r.enabled
   AND CASE r.kind
         WHEN 'metric' THEN
           l.host_id IS NOT NULL
           AND CASE WHEN r.include_offline THEN true ELSE h.status = 'up' AND l.ts > now() - interval '3 minutes' END
         WHEN 'service_check' THEN s.host_id IS NOT NULL AND (h.status = 'up' OR r.include_offline)
         ELSE h.status IN ('up', 'down') OR r.include_offline
       END;

-- +goose Down
-- Borra las reglas y alertas exposed_port (no caben en el esquema anterior) y restaura la vista de 00018.
DROP VIEW alert_candidates;
DROP INDEX inventory_changes_ports_added_idx;
DELETE FROM alerts WHERE rule_kind = 'exposed_port';
DELETE FROM alert_rules WHERE kind = 'exposed_port';
ALTER TABLE alerts DROP COLUMN detail;
ALTER TABLE alerts DROP CONSTRAINT alerts_rule_kind_check;
ALTER TABLE alerts ADD CONSTRAINT alerts_rule_kind_check CHECK (rule_kind IN ('metric', 'host_status', 'no_data', 'service_check'));
ALTER TABLE alert_rules DROP CONSTRAINT alert_rules_shape_check;
ALTER TABLE alert_rules DROP CONSTRAINT alert_rules_kind_check;
ALTER TABLE alert_rules DROP CONSTRAINT alert_rules_expected_ports_check;
ALTER TABLE alert_rules DROP COLUMN expected_ports;
ALTER TABLE alert_rules ADD CONSTRAINT alert_rules_kind_check CHECK (kind IN ('metric', 'host_status', 'no_data', 'service_check'));
ALTER TABLE alert_rules ADD CONSTRAINT alert_rules_shape_check CHECK (
     (check_id IS NULL AND NOT warn_is_breach AND (
       (kind = 'metric' AND metric IS NOT NULL AND op IS NOT NULL AND threshold IS NOT NULL
          AND ((metric IN ('load1', 'load5', 'load15') AND threshold BETWEEN 0 AND 1000000
                  AND (clear_threshold IS NULL OR clear_threshold BETWEEN 0 AND 1000000))
            OR (metric NOT IN ('load1', 'load5', 'load15') AND threshold BETWEEN 0 AND 100
                  AND (clear_threshold IS NULL OR clear_threshold BETWEEN 0 AND 100)))
          AND (clear_threshold IS NULL OR (op = 'gt' AND clear_threshold <= threshold) OR (op = 'lt' AND clear_threshold >= threshold)))
    OR (kind = 'host_status' AND metric IS NULL AND op IS NULL AND threshold IS NULL AND clear_threshold IS NULL)
    OR (kind = 'no_data' AND metric IS NULL AND op IS NULL AND clear_threshold IS NULL AND threshold BETWEEN 60 AND 2592000)))
  OR (kind = 'service_check' AND check_id IS NOT NULL AND metric IS NULL AND op IS NULL AND threshold IS NULL AND clear_threshold IS NULL));

CREATE VIEW alert_candidates AS
SELECT r.id AS rule_id, h.id AS host_id, x.value, x.breach, x.clear, x.breach_since,
       EXISTS (SELECT 1 FROM maintenance_windows m
                WHERE m.starts_at <= now() AND m.ends_at > now()
                  AND (m.scope_type = 'all'
                       OR (m.scope_type = 'group' AND m.scope_group_id = h.group_id))
       ) AS in_maintenance
  FROM alert_rules r
  JOIN hosts h ON h.status NOT IN ('disabled', 'pending')
              AND (r.scope_type = 'all'
                   OR (r.scope_type = 'group' AND r.scope_group_id = h.group_id))
  LEFT JOIN host_latest l ON l.host_id = h.id
  LEFT JOIN service_check_defs d ON d.id = r.check_id
  LEFT JOIN service_check_state s ON s.host_id = h.id AND s.check_id = r.check_id
 CROSS JOIN LATERAL (
   SELECT v.value,
          CASE r.kind
            WHEN 'metric' THEN v.value IS NOT NULL
                 AND (CASE r.op WHEN 'gt' THEN v.value > r.threshold ELSE v.value < r.threshold END)
                 AND (r.metric <> 'battery_pct' OR l.battery_charging IS NOT TRUE)
            WHEN 'host_status' THEN h.status = 'down'
            WHEN 'service_check' THEN s.ts >= now() - make_interval(secs => 3 * d.interval_seconds)
                 AND (s.status = 'down' OR (r.warn_is_breach AND s.status = 'warn'))
            ELSE h.last_seen_at IS NOT NULL AND h.last_seen_at < now() - make_interval(secs => r.threshold)
          END AS breach,
          CASE r.kind
            WHEN 'metric' THEN v.value IS NOT NULL
                 AND ((r.metric = 'battery_pct' AND l.battery_charging IS TRUE)
                      OR (CASE r.op WHEN 'gt' THEN v.value <= COALESCE(r.clear_threshold, r.threshold)
                                    ELSE v.value >= COALESCE(r.clear_threshold, r.threshold) END))
            WHEN 'host_status' THEN h.status <> 'down'
            WHEN 'service_check' THEN s.ts >= now() - make_interval(secs => 3 * d.interval_seconds)
                 AND (s.status = 'up' OR (NOT r.warn_is_breach AND s.status = 'warn'))
            ELSE h.last_seen_at IS NOT NULL AND h.last_seen_at >= now() - make_interval(secs => r.threshold)
          END AS clear,
          CASE r.kind
            WHEN 'host_status' THEN h.status_changed_at
            WHEN 'service_check' THEN s.status_changed_at
            WHEN 'no_data' THEN h.last_seen_at + make_interval(secs => r.threshold)
          END AS breach_since
     FROM (SELECT (CASE r.kind
                     WHEN 'metric' THEN
                       CASE r.metric
                         WHEN 'cpu_pct' THEN l.cpu_pct::double precision
                         WHEN 'mem_pct' THEN CASE WHEN l.mem_total_mb > 0 THEN l.mem_used_mb * 100.0 / l.mem_total_mb END::double precision
                         WHEN 'disk_max_pct' THEN CASE WHEN r.include_offline OR l.disk_ts > now() - interval '3 hours' THEN l.disk_max_pct END::double precision
                         WHEN 'battery_pct' THEN l.battery_pct::double precision
                         WHEN 'load1' THEN l.load1::double precision
                         WHEN 'load5' THEN l.load5::double precision
                         WHEN 'load15' THEN l.load15::double precision
                       END
                     WHEN 'no_data' THEN extract(epoch FROM now() - h.last_seen_at)::double precision
                     WHEN 'service_check' THEN COALESCE(s.value, s.response_ms)::double precision
                   END) AS value) v
 ) x
 WHERE r.enabled
   AND CASE r.kind
         WHEN 'metric' THEN
           l.host_id IS NOT NULL
           AND CASE WHEN r.include_offline THEN true ELSE h.status = 'up' AND l.ts > now() - interval '3 minutes' END
         WHEN 'service_check' THEN s.host_id IS NOT NULL AND (h.status = 'up' OR r.include_offline)
         ELSE h.status IN ('up', 'down') OR r.include_offline
       END;
