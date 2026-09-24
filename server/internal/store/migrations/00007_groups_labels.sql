-- +goose Up
-- Diseno de data-infra-engineer + decisiones del usuario:
--  * always_on: el grupo decide si un host que deja de reportar pasa a 'down' (siempre encendido) o a 'offline' (normal).
--  * borrar un grupo con hosts se BLOQUEA (RESTRICT): la app responde 409 group_not_empty.
CREATE TABLE host_groups (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 64 AND name = btrim(name) AND name !~ '[[:cntrl:]]'),
  down_after_seconds INT CHECK (down_after_seconds BETWEEN 30 AND 2592000),  -- NULL = usa el global (ARGOS_HOST_DOWN_AFTER)
  always_on BOOLEAN NOT NULL DEFAULT false,                                   -- true: se muestra 'down'; false: 'offline'
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX host_groups_name_lower_key ON host_groups (lower(name));

ALTER TABLE hosts ADD COLUMN group_id UUID REFERENCES host_groups(id) ON DELETE RESTRICT;
CREATE INDEX hosts_group_id_idx ON hosts (group_id) WHERE group_id IS NOT NULL;

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

-- El estado 'offline' de hosts.status lo habilita 00012_host_status_offline.sql.

-- +goose Down
DROP TRIGGER host_labels_limit ON host_labels;
DROP FUNCTION host_labels_limit();
DROP TABLE host_labels;
DROP INDEX hosts_group_id_idx;
ALTER TABLE hosts DROP COLUMN group_id;
DROP TABLE host_groups;
