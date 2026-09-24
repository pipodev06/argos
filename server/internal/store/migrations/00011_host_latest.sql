-- +goose Up
CREATE TABLE host_latest (
  host_id UUID PRIMARY KEY REFERENCES hosts(id) ON DELETE CASCADE,
  ts TIMESTAMPTZ NOT NULL,
  cpu_pct REAL, mem_used_mb INT, mem_total_mb INT, swap_used_mb INT, swap_total_mb INT,
  load1 REAL, load5 REAL, load15 REAL, battery_pct SMALLINT, battery_charging BOOLEAN,
  disk_max_pct REAL, disk_max_mount TEXT, disk_ts TIMESTAMPTZ
) WITH (fillfactor = 50);

INSERT INTO host_latest (host_id, ts, cpu_pct, mem_used_mb, mem_total_mb)
SELECT DISTINCT ON (host_id) host_id, ts, cpu_pct, mem_used_mb, mem_total_mb
  FROM metrics WHERE ts > now() - interval '24 hours' ORDER BY host_id, ts DESC;
-- +goose Down
DROP TABLE host_latest;
