-- +goose Up
-- Grupo con el que queda el host que canjea el enroll token (opcional). SET NULL: borrar un grupo no
-- debe fallar por un token pendiente; ese host simplemente se enrola sin grupo.
ALTER TABLE enroll_tokens ADD COLUMN group_id UUID REFERENCES host_groups(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE enroll_tokens DROP COLUMN group_id;
