-- +goose Up
-- Nuevo estado 'offline': un host que deja de reportar y NO pertenece a un grupo always_on
-- (laptops que se apagan o duermen) pasa a 'offline' en vez de 'down'. Solo 'down' es "requiere atencion".
-- hosts_revoked_disabled_check (00004) no cambia: un token revocado sigue exigiendo status 'disabled'.
ALTER TABLE hosts DROP CONSTRAINT hosts_status_check;
ALTER TABLE hosts
  ADD CONSTRAINT hosts_status_check CHECK (status IN ('pending', 'up', 'down', 'offline', 'disabled'));

-- +goose Down
-- El vocabulario viejo no tiene 'offline': esos hosts vuelven a 'down' (status_changed_at intacto).
UPDATE hosts SET status = 'down' WHERE status = 'offline';
ALTER TABLE hosts DROP CONSTRAINT hosts_status_check;
ALTER TABLE hosts
  ADD CONSTRAINT hosts_status_check CHECK (status IN ('pending', 'up', 'down', 'disabled'));
