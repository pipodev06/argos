-- +goose Up
-- Alcance "host" en los chequeos de servicio: un chequeo tcp, http, tls_cert o service asignado a UN host con agente
-- (como el alcance 'device' del ping/snmp, reutiliza scope_host_id, FK con ON DELETE CASCADE hacia hosts).
ALTER TABLE service_check_defs DROP CONSTRAINT service_check_defs_scope_type_check;
ALTER TABLE service_check_defs DROP CONSTRAINT scd_scope_check;
ALTER TABLE service_check_defs
  ADD CONSTRAINT service_check_defs_scope_type_check CHECK (scope_type IN ('all', 'group', 'device', 'host')),
  ADD CONSTRAINT scd_scope_check CHECK (
       (scope_type = 'all'    AND scope_group_id IS NULL AND scope_host_id IS NULL)
    OR (scope_type = 'group'  AND scope_group_id IS NOT NULL AND scope_host_id IS NULL)
    OR (scope_type IN ('device', 'host') AND scope_group_id IS NULL AND scope_host_id IS NOT NULL)),
  ADD CONSTRAINT scd_host_scope_check CHECK (scope_type <> 'host' OR type IN ('tcp', 'http', 'tls_cert', 'service'));

-- +goose Down
-- Se pierden los chequeos con alcance host y todo lo que cuelga de ellos (estado, eventos, alertas).
DELETE FROM alert_rules WHERE check_id IN (SELECT id FROM service_check_defs WHERE scope_type = 'host');
DELETE FROM service_check_defs WHERE scope_type = 'host';
ALTER TABLE service_check_defs DROP CONSTRAINT scd_host_scope_check;
ALTER TABLE service_check_defs DROP CONSTRAINT scd_scope_check;
ALTER TABLE service_check_defs DROP CONSTRAINT service_check_defs_scope_type_check;
ALTER TABLE service_check_defs
  ADD CONSTRAINT service_check_defs_scope_type_check CHECK (scope_type IN ('all', 'group', 'device')),
  ADD CONSTRAINT scd_scope_check CHECK (
       (scope_type = 'all'    AND scope_group_id IS NULL AND scope_host_id IS NULL)
    OR (scope_type = 'group'  AND scope_group_id IS NOT NULL AND scope_host_id IS NULL)
    OR (scope_type = 'device' AND scope_group_id IS NULL AND scope_host_id IS NOT NULL));
