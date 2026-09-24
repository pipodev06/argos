-- +goose Up
CREATE TABLE disk_samples (
  ts TIMESTAMPTZ NOT NULL,
  host_id UUID NOT NULL REFERENCES hosts(id),
  used_mb INT NOT NULL CHECK (used_mb >= 0),
  total_mb INT NOT NULL CHECK (total_mb > 0),
  mount TEXT NOT NULL CHECK (char_length(mount) BETWEEN 1 AND 128 AND mount !~ '[[:cntrl:]]'),
  PRIMARY KEY (host_id, mount, ts)
) PARTITION BY RANGE (ts);

-- +goose StatementBegin
DO $mig$
DECLARE
  d timestamp := date_trunc('day', now() AT TIME ZONE 'UTC');
  n int;
BEGIN
  EXECUTE format('CREATE TABLE disk_samples_legacy PARTITION OF disk_samples FOR VALUES FROM (MINVALUE) TO (%L)', d AT TIME ZONE 'UTC');
  FOR n IN 0..6 LOOP
    EXECUTE format('CREATE TABLE %I PARTITION OF disk_samples FOR VALUES FROM (%L) TO (%L)',
      'disk_samples_p' || to_char(d + n * interval '1 day', 'YYYYMMDD'),
      (d + n * interval '1 day') AT TIME ZONE 'UTC', (d + (n + 1) * interval '1 day') AT TIME ZONE 'UTC');
  END LOOP;
END
$mig$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE disk_samples;
