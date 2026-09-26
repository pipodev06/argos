-- +goose Up
-- Lote L (servicio hospedado): viewers restringidos por defecto (B1), permiso de reconocer alertas (B8), canales con
-- dueño para que un cliente no reciba datos de otro (B2) e id estable de los enroll tokens pendientes (B10).
SET LOCAL lock_timeout = '5s';

-- B1: un usuario nuevo empieza sin acceso a ningun grupo (fail-closed). NO se tocan las filas existentes. Los admins
-- (el CHECK exige all_groups) se crean con all_groups = true explicito desde el server.
ALTER TABLE users ALTER COLUMN all_groups SET DEFAULT false;

-- B8: permiso de reconocer alertas de su ambito (solo viewers; un admin puede todo).
ALTER TABLE users
  ADD COLUMN can_ack BOOLEAN NOT NULL DEFAULT false,
  DROP CONSTRAINT users_scope_viewer_only,
  ADD CONSTRAINT users_scope_viewer_only
    CHECK (role = 'viewer' OR (all_groups AND NOT can_inventory AND NOT can_updates AND NOT can_ack));

-- B2: dueño del canal. NULL = canal global del admin (usable por cualquier regla). Con dueño G, solo reglas cuyo
-- alcance quede enteramente dentro de G; borrar el grupo borra sus canales (las reglas quedan sin ese canal).
ALTER TABLE notification_channels
  ADD COLUMN group_id UUID REFERENCES host_groups(id) ON DELETE CASCADE;
CREATE INDEX notification_channels_group_idx ON notification_channels (group_id) WHERE group_id IS NOT NULL;

-- B10: id estable pa listar y revocar tokens de enrolamiento sin exponer el hash (la PK sigue siendo token_hash).
-- created_at: los tokens que ya existen quedan con la hora de esta migracion (son de vida corta).
ALTER TABLE enroll_tokens ADD COLUMN id UUID NOT NULL DEFAULT gen_random_uuid(), ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT now();
CREATE UNIQUE INDEX enroll_tokens_id_key ON enroll_tokens (id);

-- +goose Down
DROP INDEX enroll_tokens_id_key;
ALTER TABLE enroll_tokens DROP COLUMN id, DROP COLUMN created_at;
DROP INDEX notification_channels_group_idx;
ALTER TABLE notification_channels DROP COLUMN group_id;
ALTER TABLE users
  DROP CONSTRAINT users_scope_viewer_only,
  ADD CONSTRAINT users_scope_viewer_only CHECK (role = 'viewer' OR (all_groups AND NOT can_inventory AND NOT can_updates)),
  DROP COLUMN can_ack;
ALTER TABLE users ALTER COLUMN all_groups SET DEFAULT true;
