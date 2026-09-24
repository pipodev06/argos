-- +goose Up
-- Autenticacion del agente busca por hash: debe ser indexado y unico.
CREATE UNIQUE INDEX IF NOT EXISTS hosts_agent_token_hash_key ON hosts (agent_token_hash);

-- Consultas de historial de checks por host y rango de tiempo.
CREATE INDEX IF NOT EXISTS service_checks_host_id_ts_idx ON service_checks (host_id, ts);

-- +goose Down
DROP INDEX service_checks_host_id_ts_idx;
DROP INDEX hosts_agent_token_hash_key;
