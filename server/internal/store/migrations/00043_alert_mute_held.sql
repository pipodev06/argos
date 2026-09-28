-- +goose Up
-- Silenciar regla: marca la alerta que abrio con la regla silenciada y sin otro motivo de retencion (ni
-- mantenimiento ni flapping). Al terminar el silencio, si sigue abierta se libera su `opened` retenido.
ALTER TABLE alerts ADD COLUMN mute_held boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE alerts DROP COLUMN mute_held;
