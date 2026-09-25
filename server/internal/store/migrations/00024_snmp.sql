-- +goose Up
-- SNMP (punto 8, tanda 2): perfiles de credenciales reutilizables, perfil asignado a un dispositivo y tipo de chequeo
-- 'snmp' (metricas fijas: reachable = sysUpTime, if_oper = ifOperStatus de una interfaz). Los secretos del perfil
-- (v3: auth_pass y priv_pass; v2c: community) van juntos en UN blob JSON cifrado (AES-256-GCM, AAD = id del perfil).
-- Un perfil version 2 existe solo si el admin lo confirma (allow_v2c en la API); v1 no existe; v3 es siempre authPriv.
CREATE TABLE snmp_profiles (
  id UUID PRIMARY KEY,
  name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80 AND name = btrim(name) AND name !~ '[[:cntrl:]]'),
  version SMALLINT NOT NULL CHECK (version IN (2, 3)),
  username TEXT CHECK (char_length(username) BETWEEN 1 AND 64 AND username !~ '[[:cntrl:][:space:]]'),
  auth_proto TEXT CHECK (auth_proto IN ('SHA256', 'SHA512')),
  priv_proto TEXT CHECK (priv_proto IN ('AES128', 'AES256')),
  secret_enc BYTEA NOT NULL,
  secret_key_id TEXT NOT NULL CHECK (char_length(secret_key_id) BETWEEN 1 AND 64),
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT snmp_profiles_shape_check CHECK (
       (version = 3 AND username IS NOT NULL AND auth_proto IS NOT NULL AND priv_proto IS NOT NULL)
    OR (version = 2 AND username IS NULL AND auth_proto IS NULL AND priv_proto IS NULL))
);
CREATE UNIQUE INDEX snmp_profiles_name_lower_key ON snmp_profiles (lower(name));

ALTER TABLE hosts
  ADD COLUMN snmp_profile_id UUID REFERENCES snmp_profiles(id) ON DELETE RESTRICT,
  ADD CONSTRAINT hosts_snmp_profile_kind_check CHECK (snmp_profile_id IS NULL OR kind = 'device');
CREATE INDEX hosts_snmp_profile_idx ON hosts (snmp_profile_id) WHERE snmp_profile_id IS NOT NULL;

ALTER TABLE service_check_defs
  ADD COLUMN snmp_metric TEXT CHECK (snmp_metric IN ('reachable', 'if_oper')),
  ADD COLUMN snmp_if_index INT CHECK (snmp_if_index BETWEEN 1 AND 65535);
ALTER TABLE service_check_defs DROP CONSTRAINT service_check_defs_type_check;
ALTER TABLE service_check_defs DROP CONSTRAINT scd_device_scope_check;
ALTER TABLE service_check_defs DROP CONSTRAINT scd_shape_check;
ALTER TABLE service_check_defs
  ADD CONSTRAINT service_check_defs_type_check CHECK (type IN ('tcp', 'http', 'tls_cert', 'service', 'ping', 'snmp')),
  ADD CONSTRAINT scd_device_scope_check CHECK (scope_type <> 'device' OR type IN ('ping', 'snmp')),
  ADD CONSTRAINT scd_snmp_cols_check CHECK (
       (type = 'snmp' AND snmp_metric IS NOT NULL AND ((snmp_metric = 'if_oper') = (snmp_if_index IS NOT NULL)))
    OR (type <> 'snmp' AND snmp_metric IS NULL AND snmp_if_index IS NULL)),
  ADD CONSTRAINT scd_shape_check CHECK (
       (type='tcp' AND target_host IS NOT NULL AND target_port IS NOT NULL AND url IS NULL AND service_name IS NULL AND expect_status IS NULL AND expect_text IS NULL AND warn_days IS NULL AND crit_days IS NULL)
    OR (type='http' AND url IS NOT NULL AND target_host IS NULL AND target_port IS NULL AND service_name IS NULL AND warn_days IS NULL AND crit_days IS NULL)
    OR (type='tls_cert' AND target_host IS NOT NULL AND target_port IS NOT NULL AND warn_days IS NOT NULL AND crit_days IS NOT NULL AND crit_days < warn_days AND url IS NULL AND service_name IS NULL AND expect_status IS NULL AND expect_text IS NULL AND warn_ms IS NULL AND crit_ms IS NULL)
    OR (type='service' AND service_name IS NOT NULL AND url IS NULL AND target_host IS NULL AND target_port IS NULL AND expect_status IS NULL AND expect_text IS NULL AND warn_ms IS NULL AND crit_ms IS NULL AND warn_days IS NULL AND crit_days IS NULL)
    OR (type='ping' AND target_host IS NULL AND target_port IS NULL AND url IS NULL AND service_name IS NULL AND expect_status IS NULL AND expect_text IS NULL AND warn_days IS NULL AND crit_days IS NULL)
    OR (type='snmp' AND target_host IS NULL AND target_port IS NULL AND url IS NULL AND service_name IS NULL AND expect_status IS NULL AND expect_text IS NULL AND warn_days IS NULL AND crit_days IS NULL AND warn_ms IS NULL AND crit_ms IS NULL));

-- +goose Down
-- Se pierden los perfiles, la asignacion a dispositivos y los chequeos snmp (con su estado, eventos y alertas).
DELETE FROM alert_rules WHERE check_id IN (SELECT id FROM service_check_defs WHERE type = 'snmp');
DELETE FROM service_check_defs WHERE type = 'snmp';
ALTER TABLE service_check_defs DROP CONSTRAINT scd_shape_check;
ALTER TABLE service_check_defs DROP CONSTRAINT scd_snmp_cols_check;
ALTER TABLE service_check_defs DROP CONSTRAINT scd_device_scope_check;
ALTER TABLE service_check_defs DROP CONSTRAINT service_check_defs_type_check;
ALTER TABLE service_check_defs DROP COLUMN snmp_if_index, DROP COLUMN snmp_metric;
ALTER TABLE service_check_defs
  ADD CONSTRAINT service_check_defs_type_check CHECK (type IN ('tcp', 'http', 'tls_cert', 'service', 'ping')),
  ADD CONSTRAINT scd_device_scope_check CHECK (scope_type <> 'device' OR type = 'ping'),
  ADD CONSTRAINT scd_shape_check CHECK (
       (type='tcp' AND target_host IS NOT NULL AND target_port IS NOT NULL AND url IS NULL AND service_name IS NULL AND expect_status IS NULL AND expect_text IS NULL AND warn_days IS NULL AND crit_days IS NULL)
    OR (type='http' AND url IS NOT NULL AND target_host IS NULL AND target_port IS NULL AND service_name IS NULL AND warn_days IS NULL AND crit_days IS NULL)
    OR (type='tls_cert' AND target_host IS NOT NULL AND target_port IS NOT NULL AND warn_days IS NOT NULL AND crit_days IS NOT NULL AND crit_days < warn_days AND url IS NULL AND service_name IS NULL AND expect_status IS NULL AND expect_text IS NULL AND warn_ms IS NULL AND crit_ms IS NULL)
    OR (type='service' AND service_name IS NOT NULL AND url IS NULL AND target_host IS NULL AND target_port IS NULL AND expect_status IS NULL AND expect_text IS NULL AND warn_ms IS NULL AND crit_ms IS NULL AND warn_days IS NULL AND crit_days IS NULL)
    OR (type='ping' AND target_host IS NULL AND target_port IS NULL AND url IS NULL AND service_name IS NULL AND expect_status IS NULL AND expect_text IS NULL AND warn_days IS NULL AND crit_days IS NULL));
DROP INDEX hosts_snmp_profile_idx;
ALTER TABLE hosts DROP CONSTRAINT hosts_snmp_profile_kind_check, DROP COLUMN snmp_profile_id;
DROP TABLE snmp_profiles;
