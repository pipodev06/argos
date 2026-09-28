-- +goose Up
-- B7: cuotas por cliente (cliente = grupo, 1 a 1). Todas nullable: NULL = sin limite propio del grupo (el tope
-- global de la plataforma, si existe para ese recurso, sigue rigiendo igual que antes; esto es un tope ADICIONAL
-- por cliente, no lo reemplaza).
ALTER TABLE host_groups
  ADD COLUMN max_hosts    INT CHECK (max_hosts    IS NULL OR max_hosts    >= 0),
  ADD COLUMN max_devices  INT CHECK (max_devices  IS NULL OR max_devices  >= 0),
  ADD COLUMN max_checks   INT CHECK (max_checks   IS NULL OR max_checks   >= 0),
  ADD COLUMN max_users    INT CHECK (max_users    IS NULL OR max_users    >= 0),
  ADD COLUMN max_rules    INT CHECK (max_rules    IS NULL OR max_rules    >= 0),
  ADD COLUMN max_channels INT CHECK (max_channels IS NULL OR max_channels >= 0);

-- +goose Down
ALTER TABLE host_groups
  DROP COLUMN max_hosts, DROP COLUMN max_devices, DROP COLUMN max_checks,
  DROP COLUMN max_users, DROP COLUMN max_rules, DROP COLUMN max_channels;
