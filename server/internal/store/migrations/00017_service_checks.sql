-- +goose Up
-- Chequeos de servicios ejecutados por el agente (tcp, http, tls_cert, service): definiciones, ultimo estado,
-- transiciones y muestras (service_checks gana check_id y value). Alertas: alert_rules.kind = 'service_check'.
-- El secreto de un chequeo http (cabecera de autenticacion) va cifrado (secret_enc/secret_key_id/header_name).

CREATE TABLE service_check_defs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80 AND name = btrim(name) AND name !~ '[[:cntrl:]]'),
  type TEXT NOT NULL CHECK (type IN ('tcp','http','tls_cert','service')),
  interval_seconds INT NOT NULL DEFAULT 60 CHECK (interval_seconds BETWEEN 30 AND 86400),
  timeout_ms INT NOT NULL DEFAULT 5000 CHECK (timeout_ms BETWEEN 200 AND 30000),
  retries SMALLINT NOT NULL DEFAULT 1 CHECK (retries BETWEEN 0 AND 3),
  target_host TEXT CHECK (char_length(target_host) BETWEEN 1 AND 253 AND target_host ~ '^[A-Za-z0-9._:-]+$'),
  target_port INT CHECK (target_port BETWEEN 1 AND 65535),
  url TEXT CHECK (char_length(url) BETWEEN 8 AND 512 AND url ~ '^https?://[^\s]+$'),
  expect_status TEXT CHECK (expect_status ~ '^[0-9]{3}(-[0-9]{3})?$'),
  expect_text TEXT CHECK (char_length(expect_text) BETWEEN 1 AND 100 AND expect_text !~ '[[:cntrl:]]'),
  service_name TEXT CHECK (service_name ~ '^[A-Za-z0-9@._:-]{1,64}$'),
  warn_ms INT CHECK (warn_ms BETWEEN 1 AND 60000),
  crit_ms INT CHECK (crit_ms BETWEEN 1 AND 60000),
  warn_days INT CHECK (warn_days BETWEEN 1 AND 365),
  crit_days INT CHECK (crit_days BETWEEN 0 AND 365),
  header_name TEXT CHECK (header_name IN ('Authorization','X-API-Key','X-Auth-Token')),
  -- Secreto cifrado (AES-256-GCM: nonce || ct || tag, AAD = id del chequeo); la API nunca lo devuelve.
  secret_enc BYTEA,
  secret_key_id TEXT CHECK (char_length(secret_key_id) BETWEEN 1 AND 64),
  scope_type TEXT NOT NULL DEFAULT 'all' CHECK (scope_type IN ('all','group','tag')),
  scope_group_id UUID REFERENCES host_groups(id) ON DELETE CASCADE,
  scope_tag TEXT CHECK (char_length(scope_tag) BETWEEN 1 AND 32 AND scope_tag = btrim(scope_tag)),
  enabled BOOLEAN NOT NULL DEFAULT true,
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT scd_scope_check CHECK (
       (scope_type='all' AND scope_group_id IS NULL AND scope_tag IS NULL)
    OR (scope_type='group' AND scope_group_id IS NOT NULL AND scope_tag IS NULL)
    OR (scope_type='tag' AND scope_group_id IS NULL AND scope_tag IS NOT NULL)),
  CONSTRAINT scd_shape_check CHECK (
       (type='tcp' AND target_host IS NOT NULL AND target_port IS NOT NULL AND url IS NULL AND service_name IS NULL AND expect_status IS NULL AND expect_text IS NULL AND warn_days IS NULL AND crit_days IS NULL)
    OR (type='http' AND url IS NOT NULL AND target_host IS NULL AND target_port IS NULL AND service_name IS NULL AND warn_days IS NULL AND crit_days IS NULL)
    OR (type='tls_cert' AND target_host IS NOT NULL AND target_port IS NOT NULL AND warn_days IS NOT NULL AND crit_days IS NOT NULL AND crit_days < warn_days AND url IS NULL AND service_name IS NULL AND expect_status IS NULL AND expect_text IS NULL AND warn_ms IS NULL AND crit_ms IS NULL)
    OR (type='service' AND service_name IS NOT NULL AND url IS NULL AND target_host IS NULL AND target_port IS NULL AND expect_status IS NULL AND expect_text IS NULL AND warn_ms IS NULL AND crit_ms IS NULL AND warn_days IS NULL AND crit_days IS NULL)),
  CONSTRAINT scd_ms_order CHECK (warn_ms IS NULL OR crit_ms IS NULL OR warn_ms < crit_ms),
  CONSTRAINT scd_timeout_interval CHECK (timeout_ms <= interval_seconds * 1000),
  -- El secreto y su cabecera solo existen juntos y solo en chequeos http.
  CONSTRAINT scd_secret_check CHECK (
       (secret_enc IS NULL AND secret_key_id IS NULL AND header_name IS NULL)
    OR (type = 'http' AND secret_enc IS NOT NULL AND secret_key_id IS NOT NULL AND header_name IS NOT NULL))
);
CREATE UNIQUE INDEX scd_name_lower_key ON service_check_defs (lower(name));
CREATE INDEX scd_scope_group_idx ON service_check_defs (scope_group_id) WHERE scope_group_id IS NOT NULL;

-- Ultimo estado por (host, chequeo). fillfactor bajo: se actualiza en cada resultado.
CREATE TABLE service_check_state (
  host_id UUID NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
  check_id UUID NOT NULL REFERENCES service_check_defs(id) ON DELETE CASCADE,
  status TEXT NOT NULL CHECK (status IN ('up','down','warn','unknown')),
  status_changed_at TIMESTAMPTZ NOT NULL,
  ts TIMESTAMPTZ NOT NULL,
  response_ms INT CHECK (response_ms BETWEEN 0 AND 600000),
  value DOUBLE PRECISION,
  detail TEXT CHECK (char_length(detail) <= 200 AND detail !~ '[[:cntrl:]]'),
  last_sample_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (host_id, check_id)
) WITH (fillfactor = 70);
CREATE INDEX scs_check_idx ON service_check_state (check_id);

-- Transiciones de estado (retencion propia, 180 d por defecto). Sin FK a la definicion: sobreviven a su borrado
-- hasta que la purga las retire.
CREATE TABLE service_check_events (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  host_id UUID NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
  check_id UUID NOT NULL,
  from_status TEXT CHECK (from_status IN ('up','down','warn','unknown')),
  to_status TEXT NOT NULL CHECK (to_status IN ('up','down','warn','unknown')),
  detail TEXT CHECK (char_length(detail) <= 200),
  at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX sce_host_check_idx ON service_check_events (host_id, check_id, at DESC, id DESC);
CREATE INDEX sce_at_idx ON service_check_events (at);

-- Muestras: filas del formato legado (name, sin check_id) o de un chequeo (check_id, value; name NULL).
ALTER TABLE service_checks ADD COLUMN check_id UUID, ADD COLUMN value DOUBLE PRECISION;
ALTER TABLE service_checks ALTER COLUMN name DROP NOT NULL;
ALTER TABLE service_checks ADD CONSTRAINT service_checks_identity_check CHECK (check_id IS NOT NULL OR name IS NOT NULL);
CREATE INDEX service_checks_host_check_ts_idx ON service_checks (host_id, check_id, ts);

-- Alertas: kind 'service_check' con check_id (la regla desaparece con su chequeo) y warn_is_breach (avisar tambien
-- en warn). El resto de campos de condicion quedan en NULL.
DROP VIEW alert_candidates;
ALTER TABLE alert_rules DROP CONSTRAINT alert_rules_kind_check;
ALTER TABLE alert_rules DROP CONSTRAINT alert_rules_shape_check;
ALTER TABLE alert_rules ADD COLUMN check_id UUID REFERENCES service_check_defs(id) ON DELETE CASCADE,
  ADD COLUMN warn_is_breach BOOLEAN NOT NULL DEFAULT false;
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
CREATE INDEX alert_rules_check_idx ON alert_rules (check_id) WHERE check_id IS NOT NULL;
ALTER TABLE alerts DROP CONSTRAINT alerts_rule_kind_check;
ALTER TABLE alerts ADD CONSTRAINT alerts_rule_kind_check CHECK (rule_kind IN ('metric', 'host_status', 'no_data', 'service_check'));

-- Vista de evaluacion (ver 00015/00016). service_check lee service_check_state: breach = down (o warn con
-- warn_is_breach), clear = up (o warn sin warn_is_breach), breach_since = status_changed_at. Un estado sin
-- actualizar hace mas de 3 intervalos del chequeo no incumple ni se recupera. Solo hosts up (o include_offline).
CREATE VIEW alert_candidates AS
SELECT r.id AS rule_id, h.id AS host_id, x.value, x.breach, x.clear, x.breach_since,
       EXISTS (SELECT 1 FROM maintenance_windows m
                WHERE m.starts_at <= now() AND m.ends_at > now()
                  AND (m.scope_type = 'all'
                       OR (m.scope_type = 'group' AND m.scope_group_id = h.group_id)
                       OR (m.scope_type = 'tag' AND EXISTS (SELECT 1 FROM host_tags mt
                             WHERE mt.host_id = h.id AND lower(mt.tag) = lower(m.scope_tag))))
       ) AS in_maintenance
  FROM alert_rules r
  JOIN hosts h ON h.status NOT IN ('disabled', 'pending')
              AND (r.scope_type = 'all'
                   OR (r.scope_type = 'group' AND r.scope_group_id = h.group_id)
                   OR (r.scope_type = 'tag' AND EXISTS (SELECT 1 FROM host_tags rt
                         WHERE rt.host_id = h.id AND lower(rt.tag) = lower(r.scope_tag))))
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

-- +goose Down
-- Se pierden los chequeos, sus estados y las reglas y alertas de tipo service_check.
DROP VIEW alert_candidates;
DELETE FROM alerts WHERE rule_kind = 'service_check';
DELETE FROM alert_rules WHERE kind = 'service_check';
ALTER TABLE alerts DROP CONSTRAINT alerts_rule_kind_check;
ALTER TABLE alerts ADD CONSTRAINT alerts_rule_kind_check CHECK (rule_kind IN ('metric', 'host_status', 'no_data'));
DROP INDEX alert_rules_check_idx;
ALTER TABLE alert_rules DROP CONSTRAINT alert_rules_shape_check;
ALTER TABLE alert_rules DROP CONSTRAINT alert_rules_kind_check;
ALTER TABLE alert_rules DROP COLUMN check_id, DROP COLUMN warn_is_breach;
ALTER TABLE alert_rules ADD CONSTRAINT alert_rules_kind_check CHECK (kind IN ('metric', 'host_status', 'no_data'));
ALTER TABLE alert_rules ADD CONSTRAINT alert_rules_shape_check CHECK (
       (kind = 'metric' AND metric IS NOT NULL AND op IS NOT NULL AND threshold IS NOT NULL
          AND ((metric IN ('load1', 'load5', 'load15') AND threshold BETWEEN 0 AND 1000000
                  AND (clear_threshold IS NULL OR clear_threshold BETWEEN 0 AND 1000000))
            OR (metric NOT IN ('load1', 'load5', 'load15') AND threshold BETWEEN 0 AND 100
                  AND (clear_threshold IS NULL OR clear_threshold BETWEEN 0 AND 100)))
          AND (clear_threshold IS NULL OR (op = 'gt' AND clear_threshold <= threshold) OR (op = 'lt' AND clear_threshold >= threshold)))
    OR (kind = 'host_status' AND metric IS NULL AND op IS NULL AND threshold IS NULL AND clear_threshold IS NULL)
    OR (kind = 'no_data' AND metric IS NULL AND op IS NULL AND clear_threshold IS NULL AND threshold BETWEEN 60 AND 2592000));

DROP INDEX service_checks_host_check_ts_idx;
ALTER TABLE service_checks DROP CONSTRAINT service_checks_identity_check;
DELETE FROM service_checks WHERE name IS NULL;
ALTER TABLE service_checks ALTER COLUMN name SET NOT NULL;
ALTER TABLE service_checks DROP COLUMN check_id, DROP COLUMN value;
DROP TABLE service_check_events;
DROP TABLE service_check_state;
DROP TABLE service_check_defs;

CREATE VIEW alert_candidates AS
SELECT r.id AS rule_id, h.id AS host_id, x.value, x.breach, x.clear, x.breach_since,
       EXISTS (SELECT 1 FROM maintenance_windows m
                WHERE m.starts_at <= now() AND m.ends_at > now()
                  AND (m.scope_type = 'all'
                       OR (m.scope_type = 'group' AND m.scope_group_id = h.group_id)
                       OR (m.scope_type = 'tag' AND EXISTS (SELECT 1 FROM host_tags mt
                             WHERE mt.host_id = h.id AND lower(mt.tag) = lower(m.scope_tag))))
       ) AS in_maintenance
  FROM alert_rules r
  JOIN hosts h ON h.status NOT IN ('disabled', 'pending')
              AND (r.scope_type = 'all'
                   OR (r.scope_type = 'group' AND r.scope_group_id = h.group_id)
                   OR (r.scope_type = 'tag' AND EXISTS (SELECT 1 FROM host_tags rt
                         WHERE rt.host_id = h.id AND lower(rt.tag) = lower(r.scope_tag))))
  LEFT JOIN host_latest l ON l.host_id = h.id
 CROSS JOIN LATERAL (
   SELECT v.value,
          CASE r.kind
            WHEN 'metric' THEN v.value IS NOT NULL
                 AND (CASE r.op WHEN 'gt' THEN v.value > r.threshold ELSE v.value < r.threshold END)
                 AND (r.metric <> 'battery_pct' OR l.battery_charging IS NOT TRUE)
            WHEN 'host_status' THEN h.status = 'down'
            ELSE h.last_seen_at IS NOT NULL AND h.last_seen_at < now() - make_interval(secs => r.threshold)
          END AS breach,
          CASE r.kind
            WHEN 'metric' THEN v.value IS NOT NULL
                 AND ((r.metric = 'battery_pct' AND l.battery_charging IS TRUE)
                      OR (CASE r.op WHEN 'gt' THEN v.value <= COALESCE(r.clear_threshold, r.threshold)
                                    ELSE v.value >= COALESCE(r.clear_threshold, r.threshold) END))
            WHEN 'host_status' THEN h.status <> 'down'
            ELSE h.last_seen_at IS NOT NULL AND h.last_seen_at >= now() - make_interval(secs => r.threshold)
          END AS clear,
          CASE r.kind
            WHEN 'host_status' THEN h.status_changed_at
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
                   END) AS value) v
 ) x
 WHERE r.enabled
   AND CASE r.kind
         WHEN 'metric' THEN
           l.host_id IS NOT NULL
           AND CASE WHEN r.include_offline THEN true ELSE h.status = 'up' AND l.ts > now() - interval '3 minutes' END
         ELSE h.status IN ('up', 'down') OR r.include_offline
       END;
