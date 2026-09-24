-- Baseline: esquema original del MVP. Idempotente (IF NOT EXISTS) a proposito:
-- la BD de desarrollo ya tenia estas tablas creadas por el esquema viejo, y
-- esta migracion debe adoptarlas sin tocar sus datos.
-- gen_random_uuid() es nativa desde Postgres 13: no hace falta pgcrypto.

-- +goose Up
CREATE TABLE IF NOT EXISTS hosts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  hostname TEXT NOT NULL,
  os TEXT,
  arch TEXT,
  agent_token_hash TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  last_seen_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS enroll_tokens (
  token TEXT PRIMARY KEY,
  hostname TEXT NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  used_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS metrics (
  host_id UUID NOT NULL REFERENCES hosts(id),
  ts TIMESTAMPTZ NOT NULL,
  cpu_pct REAL,
  mem_used_mb INT,
  mem_total_mb INT,
  net_rx_bytes BIGINT,
  net_tx_bytes BIGINT,
  PRIMARY KEY (host_id, ts)
);

CREATE TABLE IF NOT EXISTS service_checks (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  host_id UUID NOT NULL REFERENCES hosts(id),
  name TEXT NOT NULL,
  status TEXT NOT NULL,
  response_ms INT,
  ts TIMESTAMPTZ NOT NULL
);

-- +goose Down
DROP TABLE service_checks;
DROP TABLE metrics;
DROP TABLE enroll_tokens;
DROP TABLE hosts;
