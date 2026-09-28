-- +goose Up
-- Alertas intermitentes: marca la que abrio retenida por flapping. Si sigue abierta pasada la ventana (ya no oscila)
-- se libera su `opened`; hasta entonces ni mantenimiento ni silencio la liberan.
ALTER TABLE alerts ADD COLUMN flap_held boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE alerts DROP COLUMN flap_held;
