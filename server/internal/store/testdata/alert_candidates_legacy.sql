CREATE VIEW alert_candidates_legacy AS
SELECT r.id AS rule_id, h.id AS host_id, x.value, x.breach, x.clear, x.breach_since,
       EXISTS (SELECT 1 FROM maintenance_windows m
                WHERE m.starts_at <= now() AND m.ends_at > now()
                  AND (m.scope_type = 'all'
                       OR (m.scope_type = 'group' AND m.scope_group_id = h.group_id)
                       OR (m.scope_type IN ('host', 'device') AND m.scope_host_id = h.id))
       ) AS in_maintenance,
       -- Texto corto de la alerta (max 200, sin control). Solo lo ve quien tenga el permiso del dato (API).
       CASE r.kind
         WHEN 'exposed_port' THEN ep.ports
         WHEN 'pending_updates' THEN u.pending_security || ' de seguridad, ' || COALESCE(u.pending_total::text, '?') || ' en total'
         WHEN 'reboot_pending' THEN 'reinicio pendiente desde ' || to_char(u.reboot_since AT TIME ZONE 'UTC', 'YYYY-MM-DD')
         WHEN 'os_eol' THEN eo.label || ', fin de soporte ' || to_char(eo.eol_date, 'YYYY-MM-DD')
                            || CASE WHEN eo.source = 'estimate' THEN ' (estimada)' ELSE '' END
       END AS detail
  FROM alert_rules r
  JOIN hosts h ON h.status NOT IN ('disabled', 'pending')
              AND (r.scope_type = 'all'
                   OR (r.scope_type = 'group' AND r.scope_group_id = h.group_id)
                   OR (r.scope_type IN ('host', 'device') AND r.scope_host_id = h.id))
  LEFT JOIN host_latest l ON l.host_id = h.id
  LEFT JOIN service_check_defs d ON d.id = r.check_id
  LEFT JOIN service_check_state s ON s.host_id = h.id AND s.check_id = r.check_id
  -- Parches: solo se lee la foto con reglas de parches (el filtro constante evita leerla con otros kinds).
  LEFT JOIN updates_current u ON u.host_id = h.id AND r.kind IN ('pending_updates', 'reboot_pending')
  LEFT JOIN LATERAL (
    -- os_eol: fin de vida del SO del host (0 filas si el SO no se reconoce o no esta en la tabla = desconocido).
    SELECT e.label, e.eol_date, e.source
      FROM os_eol_key(h.os, (SELECT i.os_name FROM host_info i WHERE i.host_id = h.id),
                            (SELECT i.os_version FROM host_info i WHERE i.host_id = h.id)) k
      JOIN os_eol e ON e.family = k.family AND e.cycle = k.cycle
     WHERE r.kind = 'os_eol'
  ) eo ON true
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
            WHEN 'pending_updates' THEN COALESCE(u.pending_security >= r.threshold
                 AND u.security_pending_since <= now() - make_interval(days => r.duration_days), false)
            WHEN 'reboot_pending' THEN COALESCE(u.reboot_since <= now() - make_interval(days => r.threshold::int), false)
            WHEN 'os_eol' THEN COALESCE(v.value <= r.threshold, false)
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
            -- Desconocido (NULL) no recupera: solo un dato conocido del lado sano resuelve.
            WHEN 'pending_updates' THEN COALESCE(u.pending_security < r.threshold, false)
            WHEN 'reboot_pending' THEN COALESCE(NOT u.reboot_required, false)
            WHEN 'os_eol' THEN COALESCE(v.value > r.threshold, false)
            WHEN 'service_check' THEN s.ts >= now() - make_interval(secs => 3 * d.interval_seconds)
                 AND (s.status = 'up' OR (NOT r.warn_is_breach AND s.status = 'warn'))
            ELSE h.last_seen_at IS NOT NULL AND h.last_seen_at >= now() - make_interval(secs => r.threshold)
          END AS clear,
          CASE r.kind
            WHEN 'host_status' THEN h.status_changed_at
            WHEN 'service_check' THEN s.status_changed_at
            WHEN 'no_data' THEN h.last_seen_at + make_interval(secs => r.threshold)
            WHEN 'pending_updates' THEN u.security_pending_since + make_interval(days => r.duration_days)
            WHEN 'reboot_pending' THEN u.reboot_since + make_interval(days => r.threshold::int)
            WHEN 'os_eol' THEN (eo.eol_date - r.threshold::int)::timestamptz
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
                     WHEN 'pending_updates' THEN u.pending_security::double precision
                     -- reboot_pending: dias con reinicio pendiente; os_eol: dias que faltan (<= 0 = ya vencido).
                     WHEN 'reboot_pending' THEN extract(epoch FROM now() - u.reboot_since)::double precision / 86400
                     WHEN 'os_eol' THEN (eo.eol_date - (now() AT TIME ZONE 'UTC')::date)::double precision
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
