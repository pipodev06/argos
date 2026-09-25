-- +goose Up
-- Dispositivos sin agente (punto 8, tanda 1: ping). Un dispositivo es un host con kind = 'device': no tiene token de
-- agente, tiene una direccion IP y un colector (un host kind = 'agent') que lo sondea con ARGOS_PROBE_ALLOW.
-- Los chequeos de tipo 'ping' solo aplican a dispositivos (scope 'all' = todos, 'group' = los de un grupo, 'device' =
-- uno concreto) y el resto de tipos solo a agentes.
ALTER TABLE hosts ALTER COLUMN agent_token_hash DROP NOT NULL;
ALTER TABLE hosts
  ADD COLUMN kind TEXT NOT NULL DEFAULT 'agent',
  ADD COLUMN address INET,
  ADD COLUMN collector_host_id UUID REFERENCES hosts(id) ON DELETE RESTRICT;
ALTER TABLE hosts
  ADD CONSTRAINT hosts_kind_check CHECK (kind IN ('agent', 'device')),
  ADD CONSTRAINT hosts_kind_shape_check CHECK (
       (kind = 'agent'  AND agent_token_hash IS NOT NULL AND address IS NULL AND collector_host_id IS NULL)
    OR (kind = 'device' AND agent_token_hash IS NULL AND address IS NOT NULL AND collector_host_id IS NOT NULL)),
  ADD CONSTRAINT hosts_address_host_check CHECK (address IS NULL OR masklen(address) = CASE family(address) WHEN 4 THEN 32 ELSE 128 END),
  ADD CONSTRAINT hosts_collector_not_self_check CHECK (collector_host_id IS NULL OR collector_host_id <> id);
CREATE UNIQUE INDEX hosts_device_address_key ON hosts (collector_host_id, address) WHERE kind = 'device';
CREATE INDEX hosts_collector_idx ON hosts (collector_host_id) WHERE collector_host_id IS NOT NULL;

ALTER TABLE service_check_defs ADD COLUMN scope_host_id UUID REFERENCES hosts(id) ON DELETE CASCADE;
ALTER TABLE service_check_defs DROP CONSTRAINT service_check_defs_type_check;
ALTER TABLE service_check_defs DROP CONSTRAINT service_check_defs_scope_type_check;
ALTER TABLE service_check_defs DROP CONSTRAINT scd_scope_check;
ALTER TABLE service_check_defs DROP CONSTRAINT scd_shape_check;
ALTER TABLE service_check_defs
  ADD CONSTRAINT service_check_defs_type_check CHECK (type IN ('tcp', 'http', 'tls_cert', 'service', 'ping')),
  ADD CONSTRAINT service_check_defs_scope_type_check CHECK (scope_type IN ('all', 'group', 'device')),
  ADD CONSTRAINT scd_scope_check CHECK (
       (scope_type = 'all'    AND scope_group_id IS NULL AND scope_host_id IS NULL)
    OR (scope_type = 'group'  AND scope_group_id IS NOT NULL AND scope_host_id IS NULL)
    OR (scope_type = 'device' AND scope_group_id IS NULL AND scope_host_id IS NOT NULL)),
  ADD CONSTRAINT scd_device_scope_check CHECK (scope_type <> 'device' OR type = 'ping'),
  ADD CONSTRAINT scd_shape_check CHECK (
       (type='tcp' AND target_host IS NOT NULL AND target_port IS NOT NULL AND url IS NULL AND service_name IS NULL AND expect_status IS NULL AND expect_text IS NULL AND warn_days IS NULL AND crit_days IS NULL)
    OR (type='http' AND url IS NOT NULL AND target_host IS NULL AND target_port IS NULL AND service_name IS NULL AND warn_days IS NULL AND crit_days IS NULL)
    OR (type='tls_cert' AND target_host IS NOT NULL AND target_port IS NOT NULL AND warn_days IS NOT NULL AND crit_days IS NOT NULL AND crit_days < warn_days AND url IS NULL AND service_name IS NULL AND expect_status IS NULL AND expect_text IS NULL AND warn_ms IS NULL AND crit_ms IS NULL)
    OR (type='service' AND service_name IS NOT NULL AND url IS NULL AND target_host IS NULL AND target_port IS NULL AND expect_status IS NULL AND expect_text IS NULL AND warn_ms IS NULL AND crit_ms IS NULL AND warn_days IS NULL AND crit_days IS NULL)
    OR (type='ping' AND target_host IS NULL AND target_port IS NULL AND url IS NULL AND service_name IS NULL AND expect_status IS NULL AND expect_text IS NULL AND warn_days IS NULL AND crit_days IS NULL));
CREATE INDEX scd_scope_host_idx ON service_check_defs (scope_host_id) WHERE scope_host_id IS NOT NULL;

-- +goose Down
-- Se pierden los dispositivos, sus chequeos ping y todo lo que cuelga de ellos (estado, eventos, alertas).
DELETE FROM alert_rules WHERE check_id IN (SELECT id FROM service_check_defs WHERE type = 'ping');
DELETE FROM service_check_defs WHERE type = 'ping';
DELETE FROM hosts WHERE kind = 'device';
DROP INDEX scd_scope_host_idx;
ALTER TABLE service_check_defs DROP CONSTRAINT scd_shape_check;
ALTER TABLE service_check_defs DROP CONSTRAINT scd_device_scope_check;
ALTER TABLE service_check_defs DROP CONSTRAINT scd_scope_check;
ALTER TABLE service_check_defs DROP CONSTRAINT service_check_defs_scope_type_check;
ALTER TABLE service_check_defs DROP CONSTRAINT service_check_defs_type_check;
ALTER TABLE service_check_defs DROP COLUMN scope_host_id;
ALTER TABLE service_check_defs
  ADD CONSTRAINT service_check_defs_type_check CHECK (type IN ('tcp','http','tls_cert','service')),
  ADD CONSTRAINT service_check_defs_scope_type_check CHECK (scope_type IN ('all', 'group')),
  ADD CONSTRAINT scd_scope_check CHECK (
       (scope_type = 'all'   AND scope_group_id IS NULL)
    OR (scope_type = 'group' AND scope_group_id IS NOT NULL)),
  ADD CONSTRAINT scd_shape_check CHECK (
       (type='tcp' AND target_host IS NOT NULL AND target_port IS NOT NULL AND url IS NULL AND service_name IS NULL AND expect_status IS NULL AND expect_text IS NULL AND warn_days IS NULL AND crit_days IS NULL)
    OR (type='http' AND url IS NOT NULL AND target_host IS NULL AND target_port IS NULL AND service_name IS NULL AND warn_days IS NULL AND crit_days IS NULL)
    OR (type='tls_cert' AND target_host IS NOT NULL AND target_port IS NOT NULL AND warn_days IS NOT NULL AND crit_days IS NOT NULL AND crit_days < warn_days AND url IS NULL AND service_name IS NULL AND expect_status IS NULL AND expect_text IS NULL AND warn_ms IS NULL AND crit_ms IS NULL)
    OR (type='service' AND service_name IS NOT NULL AND url IS NULL AND target_host IS NULL AND target_port IS NULL AND expect_status IS NULL AND expect_text IS NULL AND warn_ms IS NULL AND crit_ms IS NULL AND warn_days IS NULL AND crit_days IS NULL));
DROP INDEX hosts_collector_idx;
DROP INDEX hosts_device_address_key;
ALTER TABLE hosts
  DROP CONSTRAINT hosts_collector_not_self_check,
  DROP CONSTRAINT hosts_address_host_check,
  DROP CONSTRAINT hosts_kind_shape_check,
  DROP CONSTRAINT hosts_kind_check;
ALTER TABLE hosts DROP COLUMN collector_host_id, DROP COLUMN address, DROP COLUMN kind;
ALTER TABLE hosts ALTER COLUMN agent_token_hash SET NOT NULL;
