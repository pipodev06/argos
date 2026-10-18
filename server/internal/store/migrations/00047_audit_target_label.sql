-- +goose Up
-- Nombre legible del objetivo guardado al registrar el evento: sobrevive al borrado del objeto.
-- audit_log es append-only (trigger audit_log_guard): no se rellenan filas viejas; esas resuelven el
-- nombre al leer (actual, detalle o el ultimo guardado del mismo objetivo).
ALTER TABLE audit_log ADD COLUMN target_label TEXT CHECK (char_length(target_label) <= 128);
CREATE INDEX audit_log_target_idx ON audit_log (target_type, target_id, id DESC) WHERE target_id IS NOT NULL;

-- +goose Down
DROP INDEX audit_log_target_idx;
ALTER TABLE audit_log DROP COLUMN target_label;
