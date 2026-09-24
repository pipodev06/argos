-- +goose Up
-- Motor de alertas (fase A) y tablas de notificaciones (las usa la fase B: canales, worker de envio, cifrado).
-- El evaluador (internal/store/alert_eval.go) lee solo hosts, host_latest y etiquetas, nunca `metrics`.

CREATE TABLE alert_rules (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80 AND name = btrim(name) AND name !~ '[[:cntrl:]]'),
  kind TEXT NOT NULL CHECK (kind IN ('metric', 'host_status', 'no_data')),
  metric TEXT CHECK (metric IN ('cpu_pct', 'mem_pct', 'disk_max_pct', 'battery_pct', 'load1', 'load5', 'load15')),
  op TEXT CHECK (op IN ('gt', 'lt')),
  -- metric: valor limite de la metrica. no_data: segundos sin reportar. host_status: NULL (dispara con status = down).
  threshold DOUBLE PRECISION,
  -- Histeresis (solo metric): la alerta se resuelve cuando el valor cruza clear_threshold (NULL = threshold).
  clear_threshold DOUBLE PRECISION,
  duration_seconds INT NOT NULL CHECK (duration_seconds BETWEEN 0 AND 86400),
  resolve_seconds INT NOT NULL DEFAULT 60 CHECK (resolve_seconds BETWEEN 0 AND 86400),
  include_offline BOOLEAN NOT NULL DEFAULT false,
  scope_type TEXT NOT NULL DEFAULT 'all' CHECK (scope_type IN ('all', 'group', 'label')),
  scope_group_id UUID REFERENCES host_groups(id) ON DELETE CASCADE,
  scope_label_key TEXT CHECK (scope_label_key ~ '^[a-z][a-z0-9_.-]{0,62}$'),
  scope_label_value TEXT CHECK (char_length(scope_label_value) BETWEEN 1 AND 128 AND scope_label_value = btrim(scope_label_value) AND scope_label_value !~ '[[:cntrl:]]'),
  severity TEXT NOT NULL CHECK (severity IN ('info', 'warn', 'critical')),
  enabled BOOLEAN NOT NULL DEFAULT true,
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT alert_rules_scope_check CHECK (
       (scope_type = 'all'   AND scope_group_id IS NULL     AND scope_label_key IS NULL     AND scope_label_value IS NULL)
    OR (scope_type = 'group' AND scope_group_id IS NOT NULL AND scope_label_key IS NULL     AND scope_label_value IS NULL)
    OR (scope_type = 'label' AND scope_group_id IS NULL     AND scope_label_key IS NOT NULL AND scope_label_value IS NOT NULL)),
  CONSTRAINT alert_rules_shape_check CHECK (
       (kind = 'metric' AND metric IS NOT NULL AND op IS NOT NULL AND threshold IS NOT NULL
          AND ((metric IN ('load1', 'load5', 'load15') AND threshold BETWEEN 0 AND 1000000
                  AND (clear_threshold IS NULL OR clear_threshold BETWEEN 0 AND 1000000))
            OR (metric NOT IN ('load1', 'load5', 'load15') AND threshold BETWEEN 0 AND 100
                  AND (clear_threshold IS NULL OR clear_threshold BETWEEN 0 AND 100)))
          AND (clear_threshold IS NULL OR (op = 'gt' AND clear_threshold <= threshold) OR (op = 'lt' AND clear_threshold >= threshold)))
    OR (kind = 'host_status' AND metric IS NULL AND op IS NULL AND threshold IS NULL AND clear_threshold IS NULL)
    OR (kind = 'no_data' AND metric IS NULL AND op IS NULL AND clear_threshold IS NULL AND threshold BETWEEN 60 AND 2592000))
);
CREATE UNIQUE INDEX alert_rules_name_lower_key ON alert_rules (lower(name));

CREATE TABLE notification_channels (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80 AND name = btrim(name) AND name !~ '[[:cntrl:]]'),
  kind TEXT NOT NULL CHECK (kind IN ('email', 'webhook')),
  config JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(config) = 'object' AND octet_length(config::text) <= 4096),
  -- Secreto cifrado (AES-256-GCM: nonce || ct || tag); la API nunca lo devuelve.
  secret_enc BYTEA,
  secret_key_id TEXT CHECK (char_length(secret_key_id) BETWEEN 1 AND 64),
  min_severity TEXT NOT NULL DEFAULT 'warn' CHECK (min_severity IN ('info', 'warn', 'critical')),
  enabled BOOLEAN NOT NULL DEFAULT true,
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK ((secret_enc IS NULL) = (secret_key_id IS NULL))
);
CREATE UNIQUE INDEX notification_channels_name_lower_key ON notification_channels (lower(name));

CREATE TABLE rule_channels (
  rule_id UUID NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
  channel_id UUID NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
  PRIMARY KEY (rule_id, channel_id)
);
CREATE INDEX rule_channels_channel_idx ON rule_channels (channel_id);

CREATE TABLE maintenance_windows (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80 AND name = btrim(name) AND name !~ '[[:cntrl:]]'),
  starts_at TIMESTAMPTZ NOT NULL,
  ends_at TIMESTAMPTZ NOT NULL,
  scope_type TEXT NOT NULL CHECK (scope_type IN ('all', 'group', 'label')),
  scope_group_id UUID REFERENCES host_groups(id) ON DELETE CASCADE,
  scope_label_key TEXT CHECK (scope_label_key ~ '^[a-z][a-z0-9_.-]{0,62}$'),
  scope_label_value TEXT CHECK (char_length(scope_label_value) BETWEEN 1 AND 128 AND scope_label_value = btrim(scope_label_value) AND scope_label_value !~ '[[:cntrl:]]'),
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_by_name TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT maintenance_windows_span_check CHECK (ends_at > starts_at AND ends_at - starts_at <= interval '31 days'),
  -- Silenciar TODA la flota mas de 24 h esta prohibido.
  CONSTRAINT maintenance_windows_all_span_check CHECK (scope_type <> 'all' OR ends_at - starts_at <= interval '24 hours'),
  CONSTRAINT maintenance_windows_scope_check CHECK (
       (scope_type = 'all'   AND scope_group_id IS NULL     AND scope_label_key IS NULL     AND scope_label_value IS NULL)
    OR (scope_type = 'group' AND scope_group_id IS NOT NULL AND scope_label_key IS NULL     AND scope_label_value IS NULL)
    OR (scope_type = 'label' AND scope_group_id IS NULL     AND scope_label_key IS NOT NULL AND scope_label_value IS NOT NULL))
);
CREATE INDEX maintenance_windows_ends_idx ON maintenance_windows (ends_at);

CREATE TABLE alerts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  rule_id UUID REFERENCES alert_rules(id) ON DELETE SET NULL,
  host_id UUID NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
  -- Snapshot de la regla al abrir: la alerta sigue siendo legible aunque la regla cambie o se borre.
  rule_name TEXT NOT NULL,
  rule_kind TEXT NOT NULL CHECK (rule_kind IN ('metric', 'host_status', 'no_data')),
  metric TEXT,
  threshold DOUBLE PRECISION,
  severity TEXT NOT NULL CHECK (severity IN ('info', 'warn', 'critical')),
  state TEXT NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'acknowledged', 'resolved')),
  opened_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  trigger_value DOUBLE PRECISION,
  peak_value DOUBLE PRECISION,
  -- true: se abrio dentro de una ventana de mantenimiento; su notificacion 'opened' esta retenida hasta que termine.
  suppressed BOOLEAN NOT NULL DEFAULT false,
  acknowledged_by UUID REFERENCES users(id) ON DELETE SET NULL,
  acknowledged_by_name TEXT,
  acknowledged_at TIMESTAMPTZ,
  resolved_at TIMESTAMPTZ,
  resolved_by UUID REFERENCES users(id) ON DELETE SET NULL,
  resolved_by_name TEXT,
  resolve_reason TEXT CHECK (resolve_reason IN ('recovered', 'manual', 'rule_disabled', 'rule_deleted', 'host_disabled')),
  -- Desde cuando el valor esta del lado "sano" (histeresis); NULL si no lo esta.
  clear_since TIMESTAMPTZ,
  CONSTRAINT alerts_resolved_check CHECK ((state = 'resolved') = (resolved_at IS NOT NULL AND resolve_reason IS NOT NULL)),
  CONSTRAINT alerts_ack_check CHECK (state <> 'acknowledged' OR acknowledged_at IS NOT NULL)
);
-- Dedupe duro: una sola alerta activa por (regla, host).
CREATE UNIQUE INDEX alerts_active_rule_host_key ON alerts (rule_id, host_id) WHERE state <> 'resolved';
CREATE INDEX alerts_cursor_idx ON alerts (opened_at DESC, id DESC);
CREATE INDEX alerts_host_idx ON alerts (host_id, opened_at DESC, id DESC);
CREATE INDEX alerts_rule_idx ON alerts (rule_id) WHERE rule_id IS NOT NULL;
CREATE INDEX alerts_resolved_at_idx ON alerts (resolved_at) WHERE state = 'resolved';

-- Solo existe mientras la condicion se incumple y todavia no cumple duration_seconds.
CREATE TABLE alert_pending (
  rule_id UUID NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
  host_id UUID NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
  since TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (rule_id, host_id)
);
CREATE INDEX alert_pending_host_idx ON alert_pending (host_id);

-- Outbox de notificaciones. seq numera los recordatorios (opened/resolved siempre 0); la UNIQUE da idempotencia.
CREATE TABLE notification_deliveries (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  alert_id UUID NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
  channel_id UUID NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
  event TEXT NOT NULL CHECK (event IN ('opened', 'resolved', 'reminder')),
  seq INT NOT NULL DEFAULT 0 CHECK (seq >= 0),
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sending', 'sent', 'dead', 'suppressed')),
  attempts INT NOT NULL DEFAULT 0,
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  lease_until TIMESTAMPTZ,
  last_error TEXT CHECK (char_length(last_error) <= 300),
  http_status INT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  sent_at TIMESTAMPTZ,
  CONSTRAINT notification_deliveries_key UNIQUE (alert_id, channel_id, event, seq)
);
CREATE INDEX notification_deliveries_due_idx ON notification_deliveries (next_attempt_at) WHERE status = 'pending';
CREATE INDEX notification_deliveries_channel_idx ON notification_deliveries (channel_id);
CREATE INDEX notification_deliveries_created_idx ON notification_deliveries (created_at);

-- Reglas de arranque, DESACTIVADAS: el admin las revisa y activa (decision del usuario).
INSERT INTO alert_rules (name, kind, metric, op, threshold, clear_threshold, duration_seconds, resolve_seconds, severity, enabled) VALUES
  ('CPU alta',     'metric', 'cpu_pct',      'gt', 90, 85, 600,  120, 'warn', false),
  ('Memoria alta', 'metric', 'mem_pct',      'gt', 90, 85, 600,  120, 'warn', false),
  ('Disco lleno',  'metric', 'disk_max_pct', 'gt', 90, 88, 1800, 300, 'warn', false),
  ('Bateria baja', 'metric', 'battery_pct',  'lt', 10, 15, 60,   60,  'info', false);
INSERT INTO alert_rules (name, kind, duration_seconds, resolve_seconds, severity, enabled) VALUES
  ('Host caido', 'host_status', 120, 60, 'critical', false);

-- Vista de evaluacion: un renglon por (regla habilitada, host evaluable). Ver internal/store/alert_eval.go.
--   value          valor observado (NULL = sin dato)
--   breach         incumple la condicion
--   clear          esta del lado sano (histeresis: clear_threshold; sin dato de metrica = false)
--   breach_since   desde cuando incumple segun los datos del host (host_status/no_data); NULL = desde ahora
--   in_maintenance hay una ventana de mantenimiento activa que cubre al host
-- Elegibilidad: sin include_offline, metricas solo de hosts up con reporte de menos de 3 minutos, y host_status /
-- no_data solo de hosts up o down; con include_offline tambien offline y se usa el ultimo valor conocido.
-- Disco: dato de menos de 3 h. Bateria: no incumple mientras carga.
CREATE VIEW alert_candidates AS
SELECT r.id AS rule_id, h.id AS host_id, x.value, x.breach, x.clear, x.breach_since,
       EXISTS (SELECT 1 FROM maintenance_windows m
                WHERE m.starts_at <= now() AND m.ends_at > now()
                  AND (m.scope_type = 'all'
                       OR (m.scope_type = 'group' AND m.scope_group_id = h.group_id)
                       OR (m.scope_type = 'label' AND EXISTS (SELECT 1 FROM host_labels ml
                             WHERE ml.host_id = h.id AND ml.key = m.scope_label_key AND ml.value = m.scope_label_value)))
       ) AS in_maintenance
  FROM alert_rules r
  JOIN hosts h ON h.status NOT IN ('disabled', 'pending')
              AND (r.scope_type = 'all'
                   OR (r.scope_type = 'group' AND r.scope_group_id = h.group_id)
                   OR (r.scope_type = 'label' AND EXISTS (SELECT 1 FROM host_labels rl
                         WHERE rl.host_id = h.id AND rl.key = r.scope_label_key AND rl.value = r.scope_label_value)))
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

-- +goose Down
DROP VIEW alert_candidates;
DROP TABLE notification_deliveries;
DROP TABLE alert_pending;
DROP TABLE alerts;
DROP TABLE maintenance_windows;
DROP TABLE rule_channels;
DROP TABLE notification_channels;
DROP TABLE alert_rules;
