-- +goose Up
-- Permisos por grupo: un viewer puede quedar restringido a un conjunto de grupos (all_groups = false).
-- Aditiva: los usuarios existentes conservan acceso total (all_groups = true) y sin permiso de inventario.
-- Los campos solo aplican a viewers: un admin siempre ve todo (el CHECK lo garantiza).
ALTER TABLE users
  ADD COLUMN all_groups BOOLEAN NOT NULL DEFAULT true,
  ADD COLUMN can_inventory BOOLEAN NOT NULL DEFAULT false,
  ADD CONSTRAINT users_scope_viewer_only CHECK (role = 'viewer' OR (all_groups AND NOT can_inventory));

CREATE TABLE user_groups (
  user_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  group_id UUID NOT NULL REFERENCES host_groups(id) ON DELETE CASCADE,
  PRIMARY KEY (user_id, group_id)
);
CREATE INDEX user_groups_group_id_idx ON user_groups (group_id);

-- +goose Down
DROP TABLE user_groups;
ALTER TABLE users
  DROP CONSTRAINT users_scope_viewer_only,
  DROP COLUMN can_inventory,
  DROP COLUMN all_groups;
