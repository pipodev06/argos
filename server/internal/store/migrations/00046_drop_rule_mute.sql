-- +goose Up
-- Se elimina "silenciar regla": columnas sin uso.
ALTER TABLE alerts DROP COLUMN mute_held;
ALTER TABLE alert_rules DROP COLUMN muted_until;

-- +goose Down
ALTER TABLE alert_rules ADD COLUMN muted_until timestamptz NULL;
ALTER TABLE alerts ADD COLUMN mute_held boolean NOT NULL DEFAULT false;
