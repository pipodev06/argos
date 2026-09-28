-- +goose Up
-- Silenciar una regla puntual (B19-B30): deja de encolar notificaciones nuevas mientras muted_until este en el
-- futuro, sin tocar la evaluacion ni el mantenimiento (alerts.suppressed / in_maintenance son otro concepto).
ALTER TABLE alert_rules ADD COLUMN muted_until timestamptz NULL;

-- +goose Down
ALTER TABLE alert_rules DROP COLUMN muted_until;
