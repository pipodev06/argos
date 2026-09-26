-- Siembra sintetica pa medir rendimiento a escala. SOLO contra una BD desechable (p. ej. argos_perf), nunca la real.
-- La BD debe estar migrada y VACIA. Uso (via scripts/perf/run.sh):
--   psql -v hosts=1000 -v days=3 -d argos_perf -f scripts/perf/seed.sql
-- Los tokens de agente son 'perf-token-NNNN' (NNNN = 0001..hosts) pa que el generador de carga pueda reportar.
\set ON_ERROR_STOP on
\timing on

-- Las particiones legacy (MINVALUE..hoy) no permiten crear dias pasados: fuera (estan vacias), y dias diarios pasados.
ALTER TABLE metrics DETACH PARTITION metrics_legacy;
DROP TABLE metrics_legacy;
ALTER TABLE disk_samples DETACH PARTITION disk_samples_legacy;
DROP TABLE disk_samples_legacy;
SELECT format('CREATE TABLE IF NOT EXISTS %I PARTITION OF %I FOR VALUES FROM (%L) TO (%L)',
              t || '_p' || to_char(d, 'YYYYMMDD'), t, d::timestamp AT TIME ZONE 'UTC', (d + 1)::timestamp AT TIME ZONE 'UTC')
  FROM generate_series(0, :days + 1) k,
       LATERAL (SELECT (now() AT TIME ZONE 'UTC')::date - k AS d) dd,
       (VALUES ('metrics'), ('disk_samples')) AS x(t)
  WHERE NOT EXISTS (SELECT 1 FROM pg_class WHERE relname = t || '_p' || to_char(d, 'YYYYMMDD'))
\gexec

INSERT INTO host_groups (name, always_on) SELECT 'grupo-' || g, g % 3 = 0 FROM generate_series(1, 10) g;

INSERT INTO hosts (hostname, os, arch, agent_token_hash, status, last_seen_at, created_at, status_changed_at, group_id, kind)
SELECT 'perf-host-' || lpad(n::text, 4, '0'), CASE WHEN n % 5 = 0 THEN 'windows' ELSE 'linux' END, 'amd64',
       encode(sha256(convert_to('perf-token-' || lpad(n::text, 4, '0'), 'UTF8')), 'hex'),
       CASE WHEN n % 50 = 0 THEN 'down' WHEN n % 33 = 0 THEN 'offline' ELSE 'up' END,
       now() - random() * interval '20 seconds', now() - interval '60 days', now() - random() * interval '3 days',
       (SELECT id FROM host_groups ORDER BY name OFFSET (n % 10) LIMIT 1), 'agent'
  FROM generate_series(1, :hosts) n;

INSERT INTO host_info (host_id, cpu_cores, cpu_threads, cpu_model, os_name, os_version, kernel_version, agent_version, ips, updated_at, privilege)
SELECT id, 4, 8, 'Synthetic CPU', CASE WHEN os = 'windows' THEN 'Microsoft Windows 11 Pro' ELSE 'ubuntu' END,
       CASE WHEN os = 'windows' THEN '25H2' ELSE '24.04' END, '6.8.0', '0.6.1', ARRAY['10.0.0.1']::inet[], now(), 'user'
  FROM hosts;

INSERT INTO host_latest (host_id, ts, cpu_pct, mem_used_mb, mem_total_mb, swap_used_mb, swap_total_mb, load1, load5, load15,
                         disk_max_pct, disk_max_mount, disk_ts)
SELECT id, now() - random() * interval '30 seconds', 100 * random(), (2000 + 6000 * random())::int, 16384, 100, 2048,
       4 * random(), 3 * random(), 2 * random(), 30 + 65 * random(), '/', now()
  FROM hosts;

-- metrics crudas: una muestra cada 30 s (intervalo por defecto del agente) durante :days dias.
INSERT INTO metrics (host_id, ts, cpu_pct, mem_used_mb, mem_total_mb, net_rx_bytes, net_tx_bytes, swap_used_mb, swap_total_mb, load1, load5, load15)
SELECT h.id, s.ts, 100 * random(), (2000 + 6000 * random())::int, 16384, (s.i * 10000)::bigint, (s.i * 4000)::bigint, 100, 2048,
       4 * random(), 3 * random(), 2 * random()
  FROM hosts h,
       LATERAL (SELECT i, date_trunc('minute', now()) - (i * interval '30 seconds') AS ts
                  FROM generate_series(0, :days * 2880) i) s;

INSERT INTO disk_samples (ts, host_id, used_mb, total_mb, mount)
SELECT date_trunc('minute', now()) - (i * interval '5 minutes'), h.id, (100000 + 200000 * random())::int, 500000, m
  FROM hosts h, generate_series(0, :days * 288) i, (VALUES ('/'), ('/data')) mm(m);

INSERT INTO metrics_hourly (host_id, hour, samples, cpu_avg, cpu_min, cpu_max, mem_used_avg_mb, mem_used_max_mb, mem_total_mb,
                            net_rx_bps_avg, net_rx_bps_max, net_tx_bps_avg, net_tx_bps_max)
SELECT h.id, date_trunc('hour', now()) - (i * interval '1 hour'), 120, 50 * random(), 1, 100 * random(), 4000, 8000, 16384, 1000, 5000, 400, 2000
  FROM hosts h, generate_series(0, 30 * 24) i;

INSERT INTO updates_current (host_id, rev, pending_total, pending_security, reboot_required, source, collected_at, received_at)
SELECT id, encode(sha256(convert_to(id::text, 'UTF8')), 'hex'), t, LEAST(t, sec), random() < 0.2, 'apt', now(), now()
  FROM (SELECT id, (random() * 60)::int AS t, (random() * 8)::int AS sec FROM hosts) x;

INSERT INTO inventory_current (host_id, module, rev, items, item_count, collected_at, received_at)
SELECT h.id, m.module, encode(sha256(convert_to(h.id::text || m.module, 'UTF8')), 'hex'), (SELECT jsonb_agg(jsonb_build_object('name', m.module || '-' || k, 'port', 1000 + k, 'proto', 'tcp', 'bind', '0.0.0.0'))
                                 FROM generate_series(1, 60) k), 60, now(), now()
  FROM hosts h, (VALUES ('ports'), ('services'), ('processes')) m(module);

ANALYZE;
SELECT (SELECT count(*) FROM hosts) hosts, (SELECT count(*) FROM metrics) metrics, (SELECT count(*) FROM disk_samples) disk_samples,
       (SELECT count(*) FROM metrics_hourly) hourly, pg_size_pretty(pg_database_size(current_database())) size;
