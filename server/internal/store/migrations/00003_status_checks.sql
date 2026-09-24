-- +goose Up
-- Vocabulario de estados. Mantener sincronizado con internal/httpapi (validacion) y web/.
ALTER TABLE hosts
  ADD CONSTRAINT hosts_status_check CHECK (status IN ('pending', 'up', 'down', 'disabled'));

ALTER TABLE service_checks
  ADD CONSTRAINT service_checks_status_check CHECK (status IN ('up', 'down', 'warn', 'unknown'));

-- +goose Down
ALTER TABLE service_checks DROP CONSTRAINT service_checks_status_check;
ALTER TABLE hosts DROP CONSTRAINT hosts_status_check;
