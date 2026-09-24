-- $1 = from (inicio de hora UTC, inclusive), $2 = to (inicio de hora UTC, exclusivo). Idempotente.
WITH s AS (
  SELECT host_id, ts, cpu_pct, mem_used_mb, mem_total_mb,
         swap_used_mb, swap_total_mb, load1, load5, load15, battery_pct,
         EXTRACT(EPOCH FROM ts - lag(ts) OVER w)::float8 AS secs,
         net_rx_bytes - lag(net_rx_bytes) OVER w AS drx,
         net_tx_bytes - lag(net_tx_bytes) OVER w AS dtx
    FROM metrics
   WHERE ts >= $1::timestamptz - interval '15 minutes' AND ts < $2::timestamptz
  WINDOW w AS (PARTITION BY host_id ORDER BY ts)
), b AS (
  SELECT *, date_bin('1 hour', ts, TIMESTAMPTZ 'epoch') AS hour
    FROM s WHERE ts >= $1::timestamptz
), lim AS (
  SELECT host_id, hour, 3 * percentile_cont(0.5) WITHIN GROUP (ORDER BY secs) AS max_gap
    FROM b WHERE secs > 0 GROUP BY host_id, hour
), agg AS (
  SELECT b.host_id, b.hour,
         count(*)::int AS samples,
         avg(cpu_pct)::real AS cpu_avg, min(cpu_pct) AS cpu_min, max(cpu_pct) AS cpu_max,
         round(avg(mem_used_mb))::int AS mem_used_avg_mb, max(mem_used_mb) AS mem_used_max_mb, max(mem_total_mb) AS mem_total_mb,
         round(avg(swap_used_mb))::int AS swap_used_avg_mb, max(swap_used_mb) AS swap_used_max_mb, max(swap_total_mb) AS swap_total_mb,
         avg(load1)::real AS load1_avg, max(load1) AS load1_max, avg(load5)::real AS load5_avg, avg(load15)::real AS load15_avg,
         avg(battery_pct)::real AS battery_pct_avg, min(battery_pct) AS battery_pct_min,
         ((sum(drx) FILTER (WHERE drx >= 0 AND secs > 0 AND secs <= max_gap))::float8
            / nullif(sum(secs) FILTER (WHERE drx >= 0 AND secs > 0 AND secs <= max_gap), 0))::real AS net_rx_bps_avg,
         (max(drx::float8 / secs) FILTER (WHERE drx >= 0 AND secs > 0 AND secs <= max_gap))::real AS net_rx_bps_max,
         ((sum(dtx) FILTER (WHERE dtx >= 0 AND secs > 0 AND secs <= max_gap))::float8
            / nullif(sum(secs) FILTER (WHERE dtx >= 0 AND secs > 0 AND secs <= max_gap), 0))::real AS net_tx_bps_avg,
         (max(dtx::float8 / secs) FILTER (WHERE dtx >= 0 AND secs > 0 AND secs <= max_gap))::real AS net_tx_bps_max
    FROM b LEFT JOIN lim USING (host_id, hour)
   GROUP BY b.host_id, b.hour
)
INSERT INTO metrics_hourly AS t (host_id, hour, samples, cpu_avg, cpu_min, cpu_max, mem_used_avg_mb, mem_used_max_mb, mem_total_mb,
                                 net_rx_bps_avg, net_rx_bps_max, net_tx_bps_avg, net_tx_bps_max,
                                 swap_used_avg_mb, swap_used_max_mb, swap_total_mb, load1_avg, load1_max, load5_avg, load15_avg,
                                 battery_pct_avg, battery_pct_min)
SELECT host_id, hour, samples, cpu_avg, cpu_min, cpu_max, mem_used_avg_mb, mem_used_max_mb, mem_total_mb,
       net_rx_bps_avg, net_rx_bps_max, net_tx_bps_avg, net_tx_bps_max,
       swap_used_avg_mb, swap_used_max_mb, swap_total_mb, load1_avg, load1_max, load5_avg, load15_avg,
       battery_pct_avg, battery_pct_min FROM agg
ON CONFLICT (host_id, hour) DO UPDATE SET
  samples = EXCLUDED.samples, cpu_avg = EXCLUDED.cpu_avg, cpu_min = EXCLUDED.cpu_min, cpu_max = EXCLUDED.cpu_max,
  mem_used_avg_mb = EXCLUDED.mem_used_avg_mb, mem_used_max_mb = EXCLUDED.mem_used_max_mb, mem_total_mb = EXCLUDED.mem_total_mb,
  net_rx_bps_avg = EXCLUDED.net_rx_bps_avg, net_rx_bps_max = EXCLUDED.net_rx_bps_max,
  net_tx_bps_avg = EXCLUDED.net_tx_bps_avg, net_tx_bps_max = EXCLUDED.net_tx_bps_max,
  swap_used_avg_mb = EXCLUDED.swap_used_avg_mb, swap_used_max_mb = EXCLUDED.swap_used_max_mb, swap_total_mb = EXCLUDED.swap_total_mb,
  load1_avg = EXCLUDED.load1_avg, load1_max = EXCLUDED.load1_max, load5_avg = EXCLUDED.load5_avg, load15_avg = EXCLUDED.load15_avg,
  battery_pct_avg = EXCLUDED.battery_pct_avg, battery_pct_min = EXCLUDED.battery_pct_min
WHERE (t.samples, t.cpu_avg, t.cpu_min, t.cpu_max, t.mem_used_avg_mb, t.mem_used_max_mb, t.mem_total_mb,
       t.net_rx_bps_avg, t.net_rx_bps_max, t.net_tx_bps_avg, t.net_tx_bps_max,
       t.swap_used_avg_mb, t.swap_used_max_mb, t.swap_total_mb, t.load1_avg, t.load1_max, t.load5_avg, t.load15_avg,
       t.battery_pct_avg, t.battery_pct_min)
   IS DISTINCT FROM
      (EXCLUDED.samples, EXCLUDED.cpu_avg, EXCLUDED.cpu_min, EXCLUDED.cpu_max, EXCLUDED.mem_used_avg_mb, EXCLUDED.mem_used_max_mb, EXCLUDED.mem_total_mb,
       EXCLUDED.net_rx_bps_avg, EXCLUDED.net_rx_bps_max, EXCLUDED.net_tx_bps_avg, EXCLUDED.net_tx_bps_max,
       EXCLUDED.swap_used_avg_mb, EXCLUDED.swap_used_max_mb, EXCLUDED.swap_total_mb, EXCLUDED.load1_avg, EXCLUDED.load1_max,
       EXCLUDED.load5_avg, EXCLUDED.load15_avg, EXCLUDED.battery_pct_avg, EXCLUDED.battery_pct_min)
