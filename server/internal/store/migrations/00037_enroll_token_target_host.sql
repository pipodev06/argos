-- +goose Up
-- B14/B15: un enroll token puede apuntar a un host existente (re-enrolar conservando su historial, o reactivar uno
-- disabled). NULL = comportamiento actual (crea un host nuevo al canjearlo). ON DELETE CASCADE: si el host se borra
-- (purga), el token pendiente que apuntaba a el ya no tiene sentido.
ALTER TABLE enroll_tokens ADD COLUMN target_host_id UUID REFERENCES hosts(id) ON DELETE CASCADE;

-- +goose Down
ALTER TABLE enroll_tokens DROP COLUMN target_host_id;
