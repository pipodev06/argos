-- +goose Up
-- os_eol_key (regexp por fila) se evaluaba para TODOS los hosts en GET /updates (230 ms con 5000 hosts) y en la
-- vista alert_candidates. La clave (family, cycle) depende solo de hosts.os y host_info.os_name/os_version, asi que
-- se guarda en hosts (no en host_info: un host darwin sin host_info igual resuelve macOS) y la mantienen triggers
-- solo cuando cambian esas entradas. La funcion os_eol_key se conserva como unica fuente de la logica.
ALTER TABLE hosts ADD COLUMN eol_family TEXT, ADD COLUMN eol_cycle TEXT;

-- +goose StatementBegin
CREATE FUNCTION hosts_set_eol_key() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  SELECT k.family, k.cycle INTO NEW.eol_family, NEW.eol_cycle
    FROM os_eol_key(NEW.os, (SELECT i.os_name FROM host_info i WHERE i.host_id = NEW.id),
                            (SELECT i.os_version FROM host_info i WHERE i.host_id = NEW.id)) k;
  IF NOT FOUND THEN
    NEW.eol_family := NULL;
    NEW.eol_cycle := NULL;
  END IF;
  RETURN NEW;
END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION host_info_refresh_eol_key() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  v_host UUID;
BEGIN
  IF TG_OP = 'DELETE' THEN v_host := OLD.host_id; ELSE v_host := NEW.host_id; END IF;
  UPDATE hosts h SET (eol_family, eol_cycle) = (
    SELECT k.family, k.cycle
      FROM os_eol_key(h.os, (SELECT i.os_name FROM host_info i WHERE i.host_id = h.id),
                            (SELECT i.os_version FROM host_info i WHERE i.host_id = h.id)) k)
   WHERE h.id = v_host;
  RETURN NULL;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER hosts_eol_key_ins BEFORE INSERT ON hosts
  FOR EACH ROW EXECUTE FUNCTION hosts_set_eol_key();
CREATE TRIGGER hosts_eol_key_upd BEFORE UPDATE OF os ON hosts
  FOR EACH ROW WHEN (OLD.os IS DISTINCT FROM NEW.os) EXECUTE FUNCTION hosts_set_eol_key();
CREATE TRIGGER host_info_eol_key_insdel AFTER INSERT OR DELETE ON host_info
  FOR EACH ROW EXECUTE FUNCTION host_info_refresh_eol_key();
CREATE TRIGGER host_info_eol_key_upd AFTER UPDATE OF os_name, os_version ON host_info
  FOR EACH ROW WHEN (OLD.os_name IS DISTINCT FROM NEW.os_name OR OLD.os_version IS DISTINCT FROM NEW.os_version)
  EXECUTE FUNCTION host_info_refresh_eol_key();

-- Relleno de lo existente.
UPDATE hosts h SET (eol_family, eol_cycle) = (
    SELECT k.family, k.cycle
      FROM os_eol_key(h.os, (SELECT i.os_name FROM host_info i WHERE i.host_id = h.id),
                            (SELECT i.os_version FROM host_info i WHERE i.host_id = h.id)) k);

-- Misma vista que 00035; solo cambia la rama os_eol, que une la clave guardada con os_eol.
DROP VIEW alert_candidates;
CREATE VIEW alert_candidates AS
-- metric: solo hosts con ultima muestra; en linea (up y muestra de <3 min) salvo include_offline.
SELECT r.id AS rule_id, h.id AS host_id, v.value,
       COALESCE(v.value IS NOT NULL
                AND (CASE r.op WHEN 'gt' THEN v.value > r.threshold ELSE v.value < r.threshold END)
                AND (r.metric <> 'battery_pct' OR l.battery_charging IS NOT TRUE), false) AS breach,
       COALESCE(v.value IS NOT NULL
                AND ((r.metric = 'battery_pct' AND l.battery_charging IS TRUE)
                     OR (CASE r.op WHEN 'gt' THEN v.value <= COALESCE(r.clear_threshold, r.threshold)
                                   ELSE v.value >= COALESCE(r.clear_threshold, r.threshold) END)), false) AS clear,
       NULL::timestamptz AS breach_since,
       EXISTS (SELECT 1 FROM maintenance_windows m
                WHERE m.starts_at <= now() AND m.ends_at > now()
                  AND (m.scope_type = 'all'
                       OR (m.scope_type = 'group' AND m.scope_group_id = h.group_id)
                       OR (m.scope_type IN ('host', 'device') AND m.scope_host_id = h.id))) AS in_maintenance,
       NULL::text AS detail
  FROM alert_rules r
  JOIN hosts h ON h.status NOT IN ('disabled', 'pending')
              AND (r.scope_type = 'all'
                   OR (r.scope_type = 'group' AND r.scope_group_id = h.group_id)
                   OR (r.scope_type IN ('host', 'device') AND r.scope_host_id = h.id))
  JOIN host_latest l ON l.host_id = h.id
 CROSS JOIN LATERAL (SELECT (CASE r.metric
                         WHEN 'cpu_pct' THEN l.cpu_pct::double precision
                         WHEN 'mem_pct' THEN CASE WHEN l.mem_total_mb > 0 THEN l.mem_used_mb * 100.0 / l.mem_total_mb END::double precision
                         WHEN 'disk_max_pct' THEN CASE WHEN r.include_offline OR l.disk_ts > now() - interval '3 hours' THEN l.disk_max_pct END::double precision
                         WHEN 'battery_pct' THEN l.battery_pct::double precision
                         WHEN 'load1' THEN l.load1::double precision
                         WHEN 'load5' THEN l.load5::double precision
                         WHEN 'load15' THEN l.load15::double precision
                       END) AS value) v
 WHERE r.enabled AND r.kind = 'metric'
   AND (r.include_offline OR (h.status = 'up' AND l.ts > now() - interval '3 minutes'))

UNION ALL
-- host_status: incumple con el host caido.
SELECT r.id, h.id, NULL::double precision, h.status = 'down', h.status <> 'down', h.status_changed_at,
       EXISTS (SELECT 1 FROM maintenance_windows m
                WHERE m.starts_at <= now() AND m.ends_at > now()
                  AND (m.scope_type = 'all'
                       OR (m.scope_type = 'group' AND m.scope_group_id = h.group_id)
                       OR (m.scope_type IN ('host', 'device') AND m.scope_host_id = h.id))),
       NULL::text
  FROM alert_rules r
  JOIN hosts h ON h.status NOT IN ('disabled', 'pending')
              AND (r.scope_type = 'all'
                   OR (r.scope_type = 'group' AND r.scope_group_id = h.group_id)
                   OR (r.scope_type IN ('host', 'device') AND r.scope_host_id = h.id))
 WHERE r.enabled AND r.kind = 'host_status' AND (h.status IN ('up', 'down') OR r.include_offline)

UNION ALL
-- no_data: el host lleva mas de threshold segundos sin reportar.
SELECT r.id, h.id, extract(epoch FROM now() - h.last_seen_at)::double precision,
       h.last_seen_at IS NOT NULL AND h.last_seen_at < now() - make_interval(secs => r.threshold),
       h.last_seen_at IS NOT NULL AND h.last_seen_at >= now() - make_interval(secs => r.threshold),
       h.last_seen_at + make_interval(secs => r.threshold),
       EXISTS (SELECT 1 FROM maintenance_windows m
                WHERE m.starts_at <= now() AND m.ends_at > now()
                  AND (m.scope_type = 'all'
                       OR (m.scope_type = 'group' AND m.scope_group_id = h.group_id)
                       OR (m.scope_type IN ('host', 'device') AND m.scope_host_id = h.id))),
       NULL::text
  FROM alert_rules r
  JOIN hosts h ON h.status NOT IN ('disabled', 'pending')
              AND (r.scope_type = 'all'
                   OR (r.scope_type = 'group' AND r.scope_group_id = h.group_id)
                   OR (r.scope_type IN ('host', 'device') AND r.scope_host_id = h.id))
 WHERE r.enabled AND r.kind = 'no_data' AND (h.status IN ('up', 'down') OR r.include_offline)

UNION ALL
-- service_check: solo hosts con estado del chequeo; el dato caduca a los 3 intervalos.
SELECT r.id, h.id, COALESCE(s.value, s.response_ms)::double precision,
       s.ts >= now() - make_interval(secs => 3 * d.interval_seconds)
         AND (s.status = 'down' OR (r.warn_is_breach AND s.status = 'warn')),
       s.ts >= now() - make_interval(secs => 3 * d.interval_seconds)
         AND (s.status = 'up' OR (NOT r.warn_is_breach AND s.status = 'warn')),
       s.status_changed_at,
       EXISTS (SELECT 1 FROM maintenance_windows m
                WHERE m.starts_at <= now() AND m.ends_at > now()
                  AND (m.scope_type = 'all'
                       OR (m.scope_type = 'group' AND m.scope_group_id = h.group_id)
                       OR (m.scope_type IN ('host', 'device') AND m.scope_host_id = h.id))),
       NULL::text
  FROM alert_rules r
  JOIN hosts h ON h.status NOT IN ('disabled', 'pending')
              AND (r.scope_type = 'all'
                   OR (r.scope_type = 'group' AND r.scope_group_id = h.group_id)
                   OR (r.scope_type IN ('host', 'device') AND r.scope_host_id = h.id))
  JOIN service_check_defs d ON d.id = r.check_id
  JOIN service_check_state s ON s.host_id = h.id AND s.check_id = r.check_id
 WHERE r.enabled AND r.kind = 'service_check' AND (h.status = 'up' OR r.include_offline)

UNION ALL
-- exposed_port: puertos EXPUESTOS (bind distinto de loopback; vacio o ilegible = expuesto) anadidos despues de la
-- linea base (hay un cambio 'added' en inventory_changes) y fuera de expected_ports. Cuenta (proto, puerto)
-- distintos; ports lista hasta 5.
SELECT r.id, h.id, ep.n::double precision, ep.n > 0, ep.n = 0, NULL::timestamptz,
       EXISTS (SELECT 1 FROM maintenance_windows m
                WHERE m.starts_at <= now() AND m.ends_at > now()
                  AND (m.scope_type = 'all'
                       OR (m.scope_type = 'group' AND m.scope_group_id = h.group_id)
                       OR (m.scope_type IN ('host', 'device') AND m.scope_host_id = h.id))),
       ep.ports
  FROM alert_rules r
  JOIN hosts h ON h.status NOT IN ('disabled', 'pending')
              AND (r.scope_type = 'all'
                   OR (r.scope_type = 'group' AND r.scope_group_id = h.group_id)
                   OR (r.scope_type IN ('host', 'device') AND r.scope_host_id = h.id))
 CROSS JOIN LATERAL (
    -- Se parte de las claves 'added' del host (pocas; sin ninguna no se lee el inventario, el caso normal) y solo
    -- las entradas del inventario con esa clave llegan a las pruebas caras (regex y jsonb_path_exists): el orden
    -- de las condiciones importa, la de la clave va primero.
    SELECT count(*)::int AS n,
           array_to_string((array_agg(dp.proto || '/' || dp.port ORDER BY dp.port, dp.proto))[1:5], ', ') AS ports
      FROM (SELECT DISTINCT p.proto, p.port
              FROM (SELECT array_agg(DISTINCT ch.key) AS ks
                      FROM inventory_changes ch
                     WHERE ch.host_id = h.id AND ch.module = 'ports' AND ch.kind = 'added'
                    HAVING count(*) > 0) c
              JOIN inventory_current ic ON ic.host_id = h.id AND ic.module = 'ports'
             CROSS JOIN LATERAL jsonb_to_recordset(ic.items) AS p(proto text, bind text, port int)
             WHERE p.proto || '|' || COALESCE(p.bind, '') || '|' || p.port = ANY (c.ks)
               AND COALESCE(p.bind, '') !~ '^(127\.[0-9]+\.[0-9]+\.[0-9]+|::1|::ffff:127\.[0-9]+\.[0-9]+\.[0-9]+)$'
               AND NOT jsonb_path_exists(r.expected_ports, '$[*] ? (@.proto == $p && @.port == $n)',
                                         jsonb_build_object('p', p.proto, 'n', p.port))) dp
 ) ep
 WHERE r.enabled AND r.kind = 'exposed_port' AND (h.status IN ('up', 'down') OR r.include_offline)

UNION ALL
-- pending_updates: pendientes de SEGURIDAD >= threshold durante >= duration_days. Dato desconocido (NULL) no
-- incumple ni recupera.
SELECT r.id, h.id, u.pending_security::double precision,
       COALESCE(u.pending_security >= r.threshold
                AND u.security_pending_since <= now() - make_interval(days => r.duration_days), false),
       COALESCE(u.pending_security < r.threshold, false),
       u.security_pending_since + make_interval(days => r.duration_days),
       EXISTS (SELECT 1 FROM maintenance_windows m
                WHERE m.starts_at <= now() AND m.ends_at > now()
                  AND (m.scope_type = 'all'
                       OR (m.scope_type = 'group' AND m.scope_group_id = h.group_id)
                       OR (m.scope_type IN ('host', 'device') AND m.scope_host_id = h.id))),
       u.pending_security || ' de seguridad, ' || COALESCE(u.pending_total::text, '?') || ' en total'
  FROM alert_rules r
  JOIN hosts h ON h.status NOT IN ('disabled', 'pending')
              AND (r.scope_type = 'all'
                   OR (r.scope_type = 'group' AND r.scope_group_id = h.group_id)
                   OR (r.scope_type IN ('host', 'device') AND r.scope_host_id = h.id))
  LEFT JOIN updates_current u ON u.host_id = h.id
 WHERE r.enabled AND r.kind = 'pending_updates' AND (h.status IN ('up', 'down') OR r.include_offline)

UNION ALL
-- reboot_pending: reinicio pendiente desde hace >= threshold dias (el valor son los dias con reinicio pendiente).
SELECT r.id, h.id, extract(epoch FROM now() - u.reboot_since)::double precision / 86400,
       COALESCE(u.reboot_since <= now() - make_interval(days => r.threshold::int), false),
       COALESCE(NOT u.reboot_required, false),
       u.reboot_since + make_interval(days => r.threshold::int),
       EXISTS (SELECT 1 FROM maintenance_windows m
                WHERE m.starts_at <= now() AND m.ends_at > now()
                  AND (m.scope_type = 'all'
                       OR (m.scope_type = 'group' AND m.scope_group_id = h.group_id)
                       OR (m.scope_type IN ('host', 'device') AND m.scope_host_id = h.id))),
       'reinicio pendiente desde ' || to_char(u.reboot_since AT TIME ZONE 'UTC', 'YYYY-MM-DD')
  FROM alert_rules r
  JOIN hosts h ON h.status NOT IN ('disabled', 'pending')
              AND (r.scope_type = 'all'
                   OR (r.scope_type = 'group' AND r.scope_group_id = h.group_id)
                   OR (r.scope_type IN ('host', 'device') AND r.scope_host_id = h.id))
  LEFT JOIN updates_current u ON u.host_id = h.id
 WHERE r.enabled AND r.kind = 'reboot_pending' AND (h.status IN ('up', 'down') OR r.include_offline)

UNION ALL
-- os_eol: fin de vida del SO del host (sin fila si el SO no se reconoce o no esta en la tabla = desconocido; el
-- valor son los dias que faltan, <= 0 = ya vencido).
SELECT r.id, h.id, (eo.eol_date - (now() AT TIME ZONE 'UTC')::date)::double precision,
       COALESCE((eo.eol_date - (now() AT TIME ZONE 'UTC')::date)::double precision <= r.threshold, false),
       COALESCE((eo.eol_date - (now() AT TIME ZONE 'UTC')::date)::double precision > r.threshold, false),
       (eo.eol_date - r.threshold::int)::timestamptz,
       EXISTS (SELECT 1 FROM maintenance_windows m
                WHERE m.starts_at <= now() AND m.ends_at > now()
                  AND (m.scope_type = 'all'
                       OR (m.scope_type = 'group' AND m.scope_group_id = h.group_id)
                       OR (m.scope_type IN ('host', 'device') AND m.scope_host_id = h.id))),
       eo.label || ', fin de soporte ' || to_char(eo.eol_date, 'YYYY-MM-DD')
                || CASE WHEN eo.source = 'estimate' THEN ' (estimada)' ELSE '' END
  FROM alert_rules r
  JOIN hosts h ON h.status NOT IN ('disabled', 'pending')
              AND (r.scope_type = 'all'
                   OR (r.scope_type = 'group' AND r.scope_group_id = h.group_id)
                   OR (r.scope_type IN ('host', 'device') AND r.scope_host_id = h.id))
  LEFT JOIN os_eol eo ON eo.family = h.eol_family AND eo.cycle = h.eol_cycle
 WHERE r.enabled AND r.kind = 'os_eol' AND (h.status IN ('up', 'down') OR r.include_offline);

-- +goose Down
DROP VIEW alert_candidates;
DROP TRIGGER host_info_eol_key_upd ON host_info;
DROP TRIGGER host_info_eol_key_insdel ON host_info;
DROP TRIGGER hosts_eol_key_upd ON hosts;
DROP TRIGGER hosts_eol_key_ins ON hosts;
DROP FUNCTION host_info_refresh_eol_key();
DROP FUNCTION hosts_set_eol_key();
ALTER TABLE hosts DROP COLUMN eol_family, DROP COLUMN eol_cycle;

CREATE VIEW alert_candidates AS
-- metric: solo hosts con ultima muestra; en linea (up y muestra de <3 min) salvo include_offline.
SELECT r.id AS rule_id, h.id AS host_id, v.value,
       COALESCE(v.value IS NOT NULL
                AND (CASE r.op WHEN 'gt' THEN v.value > r.threshold ELSE v.value < r.threshold END)
                AND (r.metric <> 'battery_pct' OR l.battery_charging IS NOT TRUE), false) AS breach,
       COALESCE(v.value IS NOT NULL
                AND ((r.metric = 'battery_pct' AND l.battery_charging IS TRUE)
                     OR (CASE r.op WHEN 'gt' THEN v.value <= COALESCE(r.clear_threshold, r.threshold)
                                   ELSE v.value >= COALESCE(r.clear_threshold, r.threshold) END)), false) AS clear,
       NULL::timestamptz AS breach_since,
       EXISTS (SELECT 1 FROM maintenance_windows m
                WHERE m.starts_at <= now() AND m.ends_at > now()
                  AND (m.scope_type = 'all'
                       OR (m.scope_type = 'group' AND m.scope_group_id = h.group_id)
                       OR (m.scope_type IN ('host', 'device') AND m.scope_host_id = h.id))) AS in_maintenance,
       NULL::text AS detail
  FROM alert_rules r
  JOIN hosts h ON h.status NOT IN ('disabled', 'pending')
              AND (r.scope_type = 'all'
                   OR (r.scope_type = 'group' AND r.scope_group_id = h.group_id)
                   OR (r.scope_type IN ('host', 'device') AND r.scope_host_id = h.id))
  JOIN host_latest l ON l.host_id = h.id
 CROSS JOIN LATERAL (SELECT (CASE r.metric
                         WHEN 'cpu_pct' THEN l.cpu_pct::double precision
                         WHEN 'mem_pct' THEN CASE WHEN l.mem_total_mb > 0 THEN l.mem_used_mb * 100.0 / l.mem_total_mb END::double precision
                         WHEN 'disk_max_pct' THEN CASE WHEN r.include_offline OR l.disk_ts > now() - interval '3 hours' THEN l.disk_max_pct END::double precision
                         WHEN 'battery_pct' THEN l.battery_pct::double precision
                         WHEN 'load1' THEN l.load1::double precision
                         WHEN 'load5' THEN l.load5::double precision
                         WHEN 'load15' THEN l.load15::double precision
                       END) AS value) v
 WHERE r.enabled AND r.kind = 'metric'
   AND (r.include_offline OR (h.status = 'up' AND l.ts > now() - interval '3 minutes'))

UNION ALL
-- host_status: incumple con el host caido.
SELECT r.id, h.id, NULL::double precision, h.status = 'down', h.status <> 'down', h.status_changed_at,
       EXISTS (SELECT 1 FROM maintenance_windows m
                WHERE m.starts_at <= now() AND m.ends_at > now()
                  AND (m.scope_type = 'all'
                       OR (m.scope_type = 'group' AND m.scope_group_id = h.group_id)
                       OR (m.scope_type IN ('host', 'device') AND m.scope_host_id = h.id))),
       NULL::text
  FROM alert_rules r
  JOIN hosts h ON h.status NOT IN ('disabled', 'pending')
              AND (r.scope_type = 'all'
                   OR (r.scope_type = 'group' AND r.scope_group_id = h.group_id)
                   OR (r.scope_type IN ('host', 'device') AND r.scope_host_id = h.id))
 WHERE r.enabled AND r.kind = 'host_status' AND (h.status IN ('up', 'down') OR r.include_offline)

UNION ALL
-- no_data: el host lleva mas de threshold segundos sin reportar.
SELECT r.id, h.id, extract(epoch FROM now() - h.last_seen_at)::double precision,
       h.last_seen_at IS NOT NULL AND h.last_seen_at < now() - make_interval(secs => r.threshold),
       h.last_seen_at IS NOT NULL AND h.last_seen_at >= now() - make_interval(secs => r.threshold),
       h.last_seen_at + make_interval(secs => r.threshold),
       EXISTS (SELECT 1 FROM maintenance_windows m
                WHERE m.starts_at <= now() AND m.ends_at > now()
                  AND (m.scope_type = 'all'
                       OR (m.scope_type = 'group' AND m.scope_group_id = h.group_id)
                       OR (m.scope_type IN ('host', 'device') AND m.scope_host_id = h.id))),
       NULL::text
  FROM alert_rules r
  JOIN hosts h ON h.status NOT IN ('disabled', 'pending')
              AND (r.scope_type = 'all'
                   OR (r.scope_type = 'group' AND r.scope_group_id = h.group_id)
                   OR (r.scope_type IN ('host', 'device') AND r.scope_host_id = h.id))
 WHERE r.enabled AND r.kind = 'no_data' AND (h.status IN ('up', 'down') OR r.include_offline)

UNION ALL
-- service_check: solo hosts con estado del chequeo; el dato caduca a los 3 intervalos.
SELECT r.id, h.id, COALESCE(s.value, s.response_ms)::double precision,
       s.ts >= now() - make_interval(secs => 3 * d.interval_seconds)
         AND (s.status = 'down' OR (r.warn_is_breach AND s.status = 'warn')),
       s.ts >= now() - make_interval(secs => 3 * d.interval_seconds)
         AND (s.status = 'up' OR (NOT r.warn_is_breach AND s.status = 'warn')),
       s.status_changed_at,
       EXISTS (SELECT 1 FROM maintenance_windows m
                WHERE m.starts_at <= now() AND m.ends_at > now()
                  AND (m.scope_type = 'all'
                       OR (m.scope_type = 'group' AND m.scope_group_id = h.group_id)
                       OR (m.scope_type IN ('host', 'device') AND m.scope_host_id = h.id))),
       NULL::text
  FROM alert_rules r
  JOIN hosts h ON h.status NOT IN ('disabled', 'pending')
              AND (r.scope_type = 'all'
                   OR (r.scope_type = 'group' AND r.scope_group_id = h.group_id)
                   OR (r.scope_type IN ('host', 'device') AND r.scope_host_id = h.id))
  JOIN service_check_defs d ON d.id = r.check_id
  JOIN service_check_state s ON s.host_id = h.id AND s.check_id = r.check_id
 WHERE r.enabled AND r.kind = 'service_check' AND (h.status = 'up' OR r.include_offline)

UNION ALL
-- exposed_port: puertos EXPUESTOS (bind distinto de loopback; vacio o ilegible = expuesto) anadidos despues de la
-- linea base (hay un cambio 'added' en inventory_changes) y fuera de expected_ports. Cuenta (proto, puerto)
-- distintos; ports lista hasta 5.
SELECT r.id, h.id, ep.n::double precision, ep.n > 0, ep.n = 0, NULL::timestamptz,
       EXISTS (SELECT 1 FROM maintenance_windows m
                WHERE m.starts_at <= now() AND m.ends_at > now()
                  AND (m.scope_type = 'all'
                       OR (m.scope_type = 'group' AND m.scope_group_id = h.group_id)
                       OR (m.scope_type IN ('host', 'device') AND m.scope_host_id = h.id))),
       ep.ports
  FROM alert_rules r
  JOIN hosts h ON h.status NOT IN ('disabled', 'pending')
              AND (r.scope_type = 'all'
                   OR (r.scope_type = 'group' AND r.scope_group_id = h.group_id)
                   OR (r.scope_type IN ('host', 'device') AND r.scope_host_id = h.id))
 CROSS JOIN LATERAL (
    -- Se parte de las claves 'added' del host (pocas; sin ninguna no se lee el inventario, el caso normal) y solo
    -- las entradas del inventario con esa clave llegan a las pruebas caras (regex y jsonb_path_exists): el orden
    -- de las condiciones importa, la de la clave va primero.
    SELECT count(*)::int AS n,
           array_to_string((array_agg(dp.proto || '/' || dp.port ORDER BY dp.port, dp.proto))[1:5], ', ') AS ports
      FROM (SELECT DISTINCT p.proto, p.port
              FROM (SELECT array_agg(DISTINCT ch.key) AS ks
                      FROM inventory_changes ch
                     WHERE ch.host_id = h.id AND ch.module = 'ports' AND ch.kind = 'added'
                    HAVING count(*) > 0) c
              JOIN inventory_current ic ON ic.host_id = h.id AND ic.module = 'ports'
             CROSS JOIN LATERAL jsonb_to_recordset(ic.items) AS p(proto text, bind text, port int)
             WHERE p.proto || '|' || COALESCE(p.bind, '') || '|' || p.port = ANY (c.ks)
               AND COALESCE(p.bind, '') !~ '^(127\.[0-9]+\.[0-9]+\.[0-9]+|::1|::ffff:127\.[0-9]+\.[0-9]+\.[0-9]+)$'
               AND NOT jsonb_path_exists(r.expected_ports, '$[*] ? (@.proto == $p && @.port == $n)',
                                         jsonb_build_object('p', p.proto, 'n', p.port))) dp
 ) ep
 WHERE r.enabled AND r.kind = 'exposed_port' AND (h.status IN ('up', 'down') OR r.include_offline)

UNION ALL
-- pending_updates: pendientes de SEGURIDAD >= threshold durante >= duration_days. Dato desconocido (NULL) no
-- incumple ni recupera.
SELECT r.id, h.id, u.pending_security::double precision,
       COALESCE(u.pending_security >= r.threshold
                AND u.security_pending_since <= now() - make_interval(days => r.duration_days), false),
       COALESCE(u.pending_security < r.threshold, false),
       u.security_pending_since + make_interval(days => r.duration_days),
       EXISTS (SELECT 1 FROM maintenance_windows m
                WHERE m.starts_at <= now() AND m.ends_at > now()
                  AND (m.scope_type = 'all'
                       OR (m.scope_type = 'group' AND m.scope_group_id = h.group_id)
                       OR (m.scope_type IN ('host', 'device') AND m.scope_host_id = h.id))),
       u.pending_security || ' de seguridad, ' || COALESCE(u.pending_total::text, '?') || ' en total'
  FROM alert_rules r
  JOIN hosts h ON h.status NOT IN ('disabled', 'pending')
              AND (r.scope_type = 'all'
                   OR (r.scope_type = 'group' AND r.scope_group_id = h.group_id)
                   OR (r.scope_type IN ('host', 'device') AND r.scope_host_id = h.id))
  LEFT JOIN updates_current u ON u.host_id = h.id
 WHERE r.enabled AND r.kind = 'pending_updates' AND (h.status IN ('up', 'down') OR r.include_offline)

UNION ALL
-- reboot_pending: reinicio pendiente desde hace >= threshold dias (el valor son los dias con reinicio pendiente).
SELECT r.id, h.id, extract(epoch FROM now() - u.reboot_since)::double precision / 86400,
       COALESCE(u.reboot_since <= now() - make_interval(days => r.threshold::int), false),
       COALESCE(NOT u.reboot_required, false),
       u.reboot_since + make_interval(days => r.threshold::int),
       EXISTS (SELECT 1 FROM maintenance_windows m
                WHERE m.starts_at <= now() AND m.ends_at > now()
                  AND (m.scope_type = 'all'
                       OR (m.scope_type = 'group' AND m.scope_group_id = h.group_id)
                       OR (m.scope_type IN ('host', 'device') AND m.scope_host_id = h.id))),
       'reinicio pendiente desde ' || to_char(u.reboot_since AT TIME ZONE 'UTC', 'YYYY-MM-DD')
  FROM alert_rules r
  JOIN hosts h ON h.status NOT IN ('disabled', 'pending')
              AND (r.scope_type = 'all'
                   OR (r.scope_type = 'group' AND r.scope_group_id = h.group_id)
                   OR (r.scope_type IN ('host', 'device') AND r.scope_host_id = h.id))
  LEFT JOIN updates_current u ON u.host_id = h.id
 WHERE r.enabled AND r.kind = 'reboot_pending' AND (h.status IN ('up', 'down') OR r.include_offline)

UNION ALL
-- os_eol: fin de vida del SO del host (sin fila si el SO no se reconoce o no esta en la tabla = desconocido; el
-- valor son los dias que faltan, <= 0 = ya vencido).
SELECT r.id, h.id, (eo.eol_date - (now() AT TIME ZONE 'UTC')::date)::double precision,
       COALESCE((eo.eol_date - (now() AT TIME ZONE 'UTC')::date)::double precision <= r.threshold, false),
       COALESCE((eo.eol_date - (now() AT TIME ZONE 'UTC')::date)::double precision > r.threshold, false),
       (eo.eol_date - r.threshold::int)::timestamptz,
       EXISTS (SELECT 1 FROM maintenance_windows m
                WHERE m.starts_at <= now() AND m.ends_at > now()
                  AND (m.scope_type = 'all'
                       OR (m.scope_type = 'group' AND m.scope_group_id = h.group_id)
                       OR (m.scope_type IN ('host', 'device') AND m.scope_host_id = h.id))),
       eo.label || ', fin de soporte ' || to_char(eo.eol_date, 'YYYY-MM-DD')
                || CASE WHEN eo.source = 'estimate' THEN ' (estimada)' ELSE '' END
  FROM alert_rules r
  JOIN hosts h ON h.status NOT IN ('disabled', 'pending')
              AND (r.scope_type = 'all'
                   OR (r.scope_type = 'group' AND r.scope_group_id = h.group_id)
                   OR (r.scope_type IN ('host', 'device') AND r.scope_host_id = h.id))
  LEFT JOIN host_info hi ON hi.host_id = h.id
  -- La clave de fin de vida se calcula una vez por combinacion distinta de (os, nombre, version), no por host
  -- (os_eol_key trata NULL como vacio, por eso las claves se comparan con COALESCE).
  LEFT JOIN (
    SELECT x.os, x.os_name, x.os_version, e.label, e.eol_date, e.source
      FROM (SELECT DISTINCT COALESCE(h2.os, '') AS os, COALESCE(i.os_name, '') AS os_name, COALESCE(i.os_version, '') AS os_version
              FROM hosts h2 LEFT JOIN host_info i ON i.host_id = h2.id) x
     CROSS JOIN LATERAL os_eol_key(x.os, x.os_name, x.os_version) k
      JOIN os_eol e ON e.family = k.family AND e.cycle = k.cycle
  ) eo ON eo.os = COALESCE(h.os, '') AND eo.os_name = COALESCE(hi.os_name, '') AND eo.os_version = COALESCE(hi.os_version, '')
 WHERE r.enabled AND r.kind = 'os_eol' AND (h.status IN ('up', 'down') OR r.include_offline);
