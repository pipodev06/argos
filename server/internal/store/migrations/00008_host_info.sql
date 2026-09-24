-- +goose Up
CREATE TABLE host_info (
  host_id UUID PRIMARY KEY REFERENCES hosts(id) ON DELETE CASCADE,
  boot_time TIMESTAMPTZ,
  cpu_cores SMALLINT CHECK (cpu_cores BETWEEN 1 AND 4096),
  cpu_threads SMALLINT CHECK (cpu_threads BETWEEN 1 AND 8192),
  cpu_model TEXT CHECK (char_length(cpu_model) <= 128),
  os_name TEXT CHECK (char_length(os_name) <= 128),
  os_version TEXT CHECK (char_length(os_version) <= 128),
  kernel_version TEXT CHECK (char_length(kernel_version) <= 128),
  agent_version TEXT CHECK (char_length(agent_version) <= 32),
  ips INET[] CHECK (cardinality(ips) <= 16),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose Down
DROP TABLE host_info;
