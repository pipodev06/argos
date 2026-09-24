-- +goose Up
SET LOCAL lock_timeout = '5s';   -- el ALTER pide ACCESS EXCLUSIVE en padre + particiones
ALTER TABLE metrics
  ADD COLUMN swap_used_mb INT, ADD COLUMN swap_total_mb INT,
  ADD COLUMN load1 REAL, ADD COLUMN load5 REAL, ADD COLUMN load15 REAL,   -- NULL en Windows
  ADD COLUMN battery_pct SMALLINT, ADD COLUMN battery_charging BOOLEAN;   -- NULL sin bateria
ALTER TABLE metrics_hourly
  ADD COLUMN swap_used_avg_mb INT, ADD COLUMN swap_used_max_mb INT, ADD COLUMN swap_total_mb INT,
  ADD COLUMN load1_avg REAL, ADD COLUMN load1_max REAL, ADD COLUMN load5_avg REAL, ADD COLUMN load15_avg REAL,
  ADD COLUMN battery_pct_avg REAL, ADD COLUMN battery_pct_min SMALLINT;
-- +goose Down
ALTER TABLE metrics_hourly
  DROP COLUMN swap_used_avg_mb, DROP COLUMN swap_used_max_mb, DROP COLUMN swap_total_mb,
  DROP COLUMN load1_avg, DROP COLUMN load1_max, DROP COLUMN load5_avg, DROP COLUMN load15_avg,
  DROP COLUMN battery_pct_avg, DROP COLUMN battery_pct_min;
ALTER TABLE metrics
  DROP COLUMN swap_used_mb, DROP COLUMN swap_total_mb, DROP COLUMN load1, DROP COLUMN load5, DROP COLUMN load15,
  DROP COLUMN battery_pct, DROP COLUMN battery_charging;
