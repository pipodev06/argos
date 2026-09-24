-- +goose Up
-- Etiquetas de valor unico (host_tags) en lugar de clave=valor (host_labels). Las etiquetas existentes se BORRAN.
-- Alcance de reglas y ventanas: scope_type 'label' -> 'tag'; scope_label_key/value -> scope_tag.
-- Seguridad de datos: una regla con alcance de etiqueta se DESACTIVA (queda con alcance 'all' pero enabled=false,
-- nunca activa y global) y una ventana con alcance de etiqueta se ELIMINA. El conteo sale como NOTICE.

DROP VIEW alert_candidates;

-- +goose StatementBegin
DO $$
DECLARE n_rules INT; n_wins INT;
BEGIN
  UPDATE public.alert_rules SET enabled = false, scope_type = 'all', scope_label_key = NULL, scope_label_value = NULL, updated_at = now()
   WHERE scope_type = 'label';
  GET DIAGNOSTICS n_rules = ROW_COUNT;
  DELETE FROM public.maintenance_windows WHERE scope_type = 'label';
  GET DIAGNOSTICS n_wins = ROW_COUNT;
  RAISE NOTICE 'etiquetas: % reglas desactivadas, % ventanas eliminadas', n_rules, n_wins;
END $$;
-- +goose StatementEnd

ALTER TABLE alert_rules DROP CONSTRAINT alert_rules_scope_check;
ALTER TABLE alert_rules DROP CONSTRAINT alert_rules_scope_type_check;
ALTER TABLE alert_rules DROP COLUMN scope_label_key, DROP COLUMN scope_label_value;
ALTER TABLE alert_rules ADD COLUMN scope_tag TEXT CHECK (char_length(scope_tag) BETWEEN 1 AND 32 AND scope_tag = btrim(scope_tag) AND scope_tag !~ '[[:cntrl:]\u200e\u200f\u202a-\u202e\u2066-\u2069]');
ALTER TABLE alert_rules ADD CONSTRAINT alert_rules_scope_type_check CHECK (scope_type IN ('all', 'group', 'tag'));
ALTER TABLE alert_rules ADD CONSTRAINT alert_rules_scope_check CHECK (
       (scope_type = 'all'   AND scope_group_id IS NULL     AND scope_tag IS NULL)
    OR (scope_type = 'group' AND scope_group_id IS NOT NULL AND scope_tag IS NULL)
    OR (scope_type = 'tag'   AND scope_group_id IS NULL     AND scope_tag IS NOT NULL));

ALTER TABLE maintenance_windows DROP CONSTRAINT maintenance_windows_scope_check;
ALTER TABLE maintenance_windows DROP CONSTRAINT maintenance_windows_scope_type_check;
ALTER TABLE maintenance_windows DROP COLUMN scope_label_key, DROP COLUMN scope_label_value;
ALTER TABLE maintenance_windows ADD COLUMN scope_tag TEXT CHECK (char_length(scope_tag) BETWEEN 1 AND 32 AND scope_tag = btrim(scope_tag) AND scope_tag !~ '[[:cntrl:]\u200e\u200f\u202a-\u202e\u2066-\u2069]');
ALTER TABLE maintenance_windows ADD CONSTRAINT maintenance_windows_scope_type_check CHECK (scope_type IN ('all', 'group', 'tag'));
ALTER TABLE maintenance_windows ADD CONSTRAINT maintenance_windows_scope_check CHECK (
       (scope_type = 'all'   AND scope_group_id IS NULL     AND scope_tag IS NULL)
    OR (scope_type = 'group' AND scope_group_id IS NOT NULL AND scope_tag IS NULL)
    OR (scope_type = 'tag'   AND scope_group_id IS NULL     AND scope_tag IS NOT NULL));

DROP TRIGGER host_labels_limit ON host_labels;
DROP FUNCTION host_labels_limit();
DROP TABLE host_labels;

CREATE TABLE host_tags (
  host_id UUID NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
  tag TEXT NOT NULL CHECK (char_length(tag) BETWEEN 1 AND 32 AND tag = btrim(tag)
                           AND tag !~ '[[:cntrl:]\u200e\u200f\u202a-\u202e\u2066-\u2069]')
);
CREATE UNIQUE INDEX host_tags_host_lower_uq ON host_tags (host_id, lower(tag));
CREATE INDEX host_tags_lower_idx ON host_tags (lower(tag), host_id);

-- +goose StatementBegin
CREATE FUNCTION host_tags_limit() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
  PERFORM pg_advisory_xact_lock(hashtextextended('argos.host_tags:' || NEW.host_id::text, 0));
  IF EXISTS (SELECT 1 FROM public.host_tags WHERE host_id = NEW.host_id AND lower(tag) = lower(NEW.tag)) THEN
    RETURN NEW;   -- repetir una etiqueta existente no cuenta (lo rechaza o ignora el indice unico)
  END IF;
  IF (SELECT count(*) FROM public.host_tags WHERE host_id = NEW.host_id) >= 20 THEN
    RAISE EXCEPTION 'un host admite como maximo 20 etiquetas'
      USING ERRCODE = 'check_violation', CONSTRAINT = 'host_tags_max_per_host';
  END IF;
  RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER host_tags_limit BEFORE INSERT ON host_tags
  FOR EACH ROW EXECUTE FUNCTION host_tags_limit();

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

-- +goose Down
-- No recupera datos: las etiquetas de valor unico se pierden; reglas/ventanas con alcance 'tag' se tratan igual
-- que en Up (regla desactivada, ventana eliminada).
DROP VIEW alert_candidates;

-- +goose StatementBegin
DO $$
DECLARE n_rules INT; n_wins INT;
BEGIN
  UPDATE public.alert_rules SET enabled = false, scope_type = 'all', scope_tag = NULL, updated_at = now()
   WHERE scope_type = 'tag';
  GET DIAGNOSTICS n_rules = ROW_COUNT;
  DELETE FROM public.maintenance_windows WHERE scope_type = 'tag';
  GET DIAGNOSTICS n_wins = ROW_COUNT;
  RAISE NOTICE 'etiquetas: % reglas desactivadas, % ventanas eliminadas', n_rules, n_wins;
END $$;
-- +goose StatementEnd

DROP TRIGGER host_tags_limit ON host_tags;
DROP FUNCTION host_tags_limit();
DROP TABLE host_tags;

CREATE TABLE host_labels (
  host_id UUID NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
  key TEXT NOT NULL CHECK (key ~ '^[a-z][a-z0-9_.-]{0,62}$'),
  value TEXT NOT NULL CHECK (char_length(value) BETWEEN 1 AND 128 AND value = btrim(value) AND value !~ '[[:cntrl:]]'),
  PRIMARY KEY (host_id, key)
);
CREATE INDEX host_labels_kv_idx ON host_labels (key, value, host_id);

-- +goose StatementBegin
CREATE FUNCTION host_labels_limit() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
  PERFORM pg_advisory_xact_lock(hashtextextended('argos.host_labels:' || NEW.host_id::text, 0));
  IF EXISTS (SELECT 1 FROM public.host_labels WHERE host_id = NEW.host_id AND key = NEW.key) THEN
    RETURN NEW;   -- reescribir una clave existente no cuenta
  END IF;
  IF (SELECT count(*) FROM public.host_labels WHERE host_id = NEW.host_id) >= 20 THEN
    RAISE EXCEPTION 'un host admite como maximo 20 etiquetas'
      USING ERRCODE = 'check_violation', CONSTRAINT = 'host_labels_max_per_host';
  END IF;
  RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER host_labels_limit BEFORE INSERT ON host_labels
  FOR EACH ROW EXECUTE FUNCTION host_labels_limit();

ALTER TABLE alert_rules DROP CONSTRAINT alert_rules_scope_check;
ALTER TABLE alert_rules DROP CONSTRAINT alert_rules_scope_type_check;
ALTER TABLE alert_rules DROP COLUMN scope_tag;
ALTER TABLE alert_rules ADD COLUMN scope_label_key TEXT CHECK (scope_label_key ~ '^[a-z][a-z0-9_.-]{0,62}$'),
  ADD COLUMN scope_label_value TEXT CHECK (char_length(scope_label_value) BETWEEN 1 AND 128 AND scope_label_value = btrim(scope_label_value) AND scope_label_value !~ '[[:cntrl:]]');
ALTER TABLE alert_rules ADD CONSTRAINT alert_rules_scope_type_check CHECK (scope_type IN ('all', 'group', 'label'));
ALTER TABLE alert_rules ADD CONSTRAINT alert_rules_scope_check CHECK (
       (scope_type = 'all'   AND scope_group_id IS NULL     AND scope_label_key IS NULL     AND scope_label_value IS NULL)
    OR (scope_type = 'group' AND scope_group_id IS NOT NULL AND scope_label_key IS NULL     AND scope_label_value IS NULL)
    OR (scope_type = 'label' AND scope_group_id IS NULL     AND scope_label_key IS NOT NULL AND scope_label_value IS NOT NULL));

ALTER TABLE maintenance_windows DROP CONSTRAINT maintenance_windows_scope_check;
ALTER TABLE maintenance_windows DROP CONSTRAINT maintenance_windows_scope_type_check;
ALTER TABLE maintenance_windows DROP COLUMN scope_tag;
ALTER TABLE maintenance_windows ADD COLUMN scope_label_key TEXT CHECK (scope_label_key ~ '^[a-z][a-z0-9_.-]{0,62}$'),
  ADD COLUMN scope_label_value TEXT CHECK (char_length(scope_label_value) BETWEEN 1 AND 128 AND scope_label_value = btrim(scope_label_value) AND scope_label_value !~ '[[:cntrl:]]');
ALTER TABLE maintenance_windows ADD CONSTRAINT maintenance_windows_scope_type_check CHECK (scope_type IN ('all', 'group', 'label'));
ALTER TABLE maintenance_windows ADD CONSTRAINT maintenance_windows_scope_check CHECK (
       (scope_type = 'all'   AND scope_group_id IS NULL     AND scope_label_key IS NULL     AND scope_label_value IS NULL)
    OR (scope_type = 'group' AND scope_group_id IS NOT NULL AND scope_label_key IS NULL     AND scope_label_value IS NULL)
    OR (scope_type = 'label' AND scope_group_id IS NULL     AND scope_label_key IS NOT NULL AND scope_label_value IS NOT NULL));

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
