-- +goose Up
-- Alerta intermitente (B19-B30): la 3.a apertura de la misma regla+host en 30 min se marca flapping y su aviso
-- `opened` se encola suppressed. Es un concepto aparte de alerts.suppressed (mantenimiento), que releaseSuppressedSQL libera.
ALTER TABLE alerts ADD COLUMN flapping boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE alerts DROP COLUMN flapping;
