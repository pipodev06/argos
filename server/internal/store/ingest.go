package store

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"
)

// ServiceCheck es el resultado de un chequeo de servicio reportado por un agente.
type ServiceCheck struct {
	Name       string
	Status     string
	ResponseMs int
}

// DiskSample es el uso de un punto de montaje en el instante del reporte.
type DiskSample struct {
	Mount   string
	UsedMB  int
	TotalMB int
}

// HostInfoUpdate es la info estatica que reporta el agente. Un texto vacio o un puntero nil es
// "sin dato" y se guarda NULL. Solo se escribe si algo cambio respecto de lo guardado.
type HostInfoUpdate struct {
	BootTime      *time.Time
	CPUModel      string
	CPUCores      *int
	CPUThreads    *int
	OSName        string
	OSVersion     string
	KernelVersion string
	AgentVersion  string
	IPs           []netip.Addr
}

// Sample es un reporte completo del agente en un instante. Los campos con puntero (swap, load,
// bateria) son "ausente = sin dato": se guardan NULL, nunca cero.
type Sample struct {
	Timestamp  time.Time
	CPUPct     float64
	MemUsedMB  int
	MemTotalMB int
	NetRxBytes int64
	NetTxBytes int64

	SwapUsedMB      *int
	SwapTotalMB     *int
	Load1           *float64
	Load5           *float64
	Load15          *float64
	BatteryPct      *int
	BatteryCharging *bool

	// Disks va vacio salvo en los reportes que llevan disco (el agente lo manda cada ~5 min).
	Disks []DiskSample
	// Info es nil si el reporte no trae host_info.
	Info *HostInfoUpdate

	Services []ServiceCheck
}

// IngestResult describe el efecto de un ingest sobre el host.
type IngestResult struct {
	Hostname string
	// PreviousStatus es el status del host antes del reporte ("" si no cambio nada: host deshabilitado).
	PreviousStatus string
	// Recovered es true si el host estaba down u offline y este reporte lo devolvio a up.
	Recovered bool
}

const (
	insertMetricsSQL = `
INSERT INTO metrics (host_id, ts, cpu_pct, mem_used_mb, mem_total_mb, net_rx_bytes, net_tx_bytes,
                     swap_used_mb, swap_total_mb, load1, load5, load15, battery_pct, battery_charging)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
ON CONFLICT (host_id, ts) DO NOTHING`

	// Una sentencia por reporte con discos (unnest, no INSERTs en bucle).
	insertDisksSQL = `
INSERT INTO disk_samples (ts, host_id, used_mb, total_mb, mount)
SELECT $2::timestamptz, $1::uuid, u.used, u.total, u.mount FROM unnest($3::int[], $4::int[], $5::text[]) AS u(used, total, mount)
ON CONFLICT DO NOTHING`

	// host_latest: ultimo valor por host. La guarda l.ts < EXCLUDED.ts evita que un reporte tardio del
	// buffer offline pise uno mas nuevo. Los campos de disco se conservan si el reporte no trae discos.
	upsertLatestSQL = `
INSERT INTO host_latest AS l (host_id, ts, cpu_pct, mem_used_mb, mem_total_mb, swap_used_mb, swap_total_mb,
                              load1, load5, load15, battery_pct, battery_charging,
                              disk_max_pct, disk_max_mount, disk_ts)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
ON CONFLICT (host_id) DO UPDATE SET
  ts = EXCLUDED.ts, cpu_pct = EXCLUDED.cpu_pct, mem_used_mb = EXCLUDED.mem_used_mb, mem_total_mb = EXCLUDED.mem_total_mb,
  swap_used_mb = EXCLUDED.swap_used_mb, swap_total_mb = EXCLUDED.swap_total_mb,
  load1 = EXCLUDED.load1, load5 = EXCLUDED.load5, load15 = EXCLUDED.load15,
  battery_pct = EXCLUDED.battery_pct, battery_charging = EXCLUDED.battery_charging,
  disk_max_pct = COALESCE(EXCLUDED.disk_max_pct, l.disk_max_pct),
  disk_max_mount = CASE WHEN EXCLUDED.disk_max_pct IS NULL THEN l.disk_max_mount ELSE EXCLUDED.disk_max_mount END,
  disk_ts = COALESCE(EXCLUDED.disk_ts, l.disk_ts)
WHERE l.ts < EXCLUDED.ts`

	// host_info: no escribe si nada cambio (boot_time con tolerancia de 60 s: el agente lo deriva de uptime).
	upsertInfoSQL = `
INSERT INTO host_info AS i (host_id, boot_time, cpu_cores, cpu_threads, cpu_model, os_name, os_version,
                            kernel_version, agent_version, ips)
VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, ''), $10)
ON CONFLICT (host_id) DO UPDATE SET
  boot_time = EXCLUDED.boot_time, cpu_cores = EXCLUDED.cpu_cores, cpu_threads = EXCLUDED.cpu_threads,
  cpu_model = EXCLUDED.cpu_model, os_name = EXCLUDED.os_name, os_version = EXCLUDED.os_version,
  kernel_version = EXCLUDED.kernel_version, agent_version = EXCLUDED.agent_version, ips = EXCLUDED.ips,
  updated_at = now()
WHERE (i.cpu_cores, i.cpu_threads, i.cpu_model, i.os_name, i.os_version, i.kernel_version, i.agent_version, i.ips)
      IS DISTINCT FROM
      (EXCLUDED.cpu_cores, EXCLUDED.cpu_threads, EXCLUDED.cpu_model, EXCLUDED.os_name, EXCLUDED.os_version,
       EXCLUDED.kernel_version, EXCLUDED.agent_version, EXCLUDED.ips)
   OR (i.boot_time IS NULL) <> (EXCLUDED.boot_time IS NULL)
   OR abs(extract(epoch FROM i.boot_time - EXCLUDED.boot_time)) > 60`

	// ingestHostSQL marca el host como up y devuelve su hostname y el status previo. Bloquea
	// la fila (FOR UPDATE) pa que el status previo sea el real aunque el detector corra a la
	// vez. status_changed_at solo cambia si el status cambia (los SET leen la fila vieja) y la
	// transicion se registra en host_events en la misma sentencia.
	// Un host disabled no se reactiva solo: no devuelve fila. Desde down u offline vuelve a up.
	ingestHostSQL = `
WITH upd AS (
  UPDATE hosts h
     SET last_seen_at = now(),
         status_changed_at = CASE WHEN h.status <> 'up' THEN now() ELSE h.status_changed_at END,
         status = 'up'
    FROM (SELECT status FROM hosts WHERE id = $1 FOR UPDATE) prev
   WHERE h.id = $1 AND h.status <> 'disabled'
  RETURNING h.id, h.hostname, prev.status AS prev_status
), ev AS (
  INSERT INTO host_events (host_id, from_status, to_status, source)
  SELECT id, prev_status, 'up', 'ingest' FROM upd WHERE prev_status <> 'up'
)
SELECT hostname, prev_status FROM upd`
)

// IngestMetrics guarda un reporte (metricas, discos, servicios, info del host y ultimo valor) y
// marca el host como up, todo en una transaccion y un solo round-trip (pgx.Batch).
func (s *Store) IngestMetrics(ctx context.Context, hostID string, smp Sample) (IngestResult, error) {
	var res IngestResult
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var b pgx.Batch
		b.Queue(insertMetricsSQL, hostID, smp.Timestamp, smp.CPUPct, smp.MemUsedMB, smp.MemTotalMB,
			smp.NetRxBytes, smp.NetTxBytes, smp.SwapUsedMB, smp.SwapTotalMB,
			smp.Load1, smp.Load5, smp.Load15, smp.BatteryPct, smp.BatteryCharging)
		if len(smp.Disks) > 0 {
			used := make([]int, len(smp.Disks))
			total := make([]int, len(smp.Disks))
			mounts := make([]string, len(smp.Disks))
			for i, d := range smp.Disks {
				used[i], total[i], mounts[i] = d.UsedMB, d.TotalMB, d.Mount
			}
			b.Queue(insertDisksSQL, hostID, smp.Timestamp, used, total, mounts)
		}
		for _, c := range smp.Services {
			b.Queue(
				`INSERT INTO service_checks (host_id, name, status, response_ms, ts) VALUES ($1, $2, $3, $4, $5)`,
				hostID, c.Name, c.Status, c.ResponseMs, smp.Timestamp)
		}
		if info := smp.Info; info != nil {
			b.Queue(upsertInfoSQL, hostID, info.BootTime, info.CPUCores, info.CPUThreads, info.CPUModel,
				info.OSName, info.OSVersion, info.KernelVersion, info.AgentVersion, info.IPs)
		}
		diskPct, diskMount, diskTS := worstDisk(smp.Disks, smp.Timestamp)
		b.Queue(upsertLatestSQL, hostID, smp.Timestamp, smp.CPUPct, smp.MemUsedMB, smp.MemTotalMB,
			smp.SwapUsedMB, smp.SwapTotalMB, smp.Load1, smp.Load5, smp.Load15, smp.BatteryPct, smp.BatteryCharging,
			diskPct, diskMount, diskTS)
		b.Queue(ingestHostSQL, hostID)

		br := tx.SendBatch(ctx, &b)
		var execErr error
		for i := 0; i < b.Len()-1; i++ {
			if _, err := br.Exec(); err != nil {
				execErr = fmt.Errorf("ejecutando statement %d del batch: %w", i, err)
				break
			}
		}
		if execErr == nil {
			var prev string
			err := br.QueryRow().Scan(&res.Hostname, &prev)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				// Host deshabilitado o revocado tras autenticar: se guardan las metricas, sin tocar el status.
			case err != nil:
				execErr = fmt.Errorf("marcando host up: %w", err)
			default:
				res.PreviousStatus = prev
				res.Recovered = prev == "down" || prev == "offline"
			}
		}
		closeErr := br.Close()
		if execErr != nil {
			return execErr
		}
		return closeErr
	})
	if err != nil {
		if isPartitionMissing(err) {
			err = fmt.Errorf("%w: %w", ErrPartitionMissing, err)
		}
		return IngestResult{}, fmt.Errorf("ingest de metricas: %w", err)
	}
	return res, nil
}

// worstDisk devuelve el montaje mas lleno (porcentaje de uso) y el instante de la muestra, o nils
// si el reporte no trae discos. Alimenta host_latest.disk_max_*.
func worstDisk(disks []DiskSample, ts time.Time) (pct *float64, mount *string, at *time.Time) {
	for _, d := range disks {
		p := float64(d.UsedMB) * 100 / float64(d.TotalMB)
		if pct == nil || p > *pct {
			v, m := p, d.Mount
			pct, mount = &v, &m
		}
	}
	if pct == nil {
		return nil, nil, nil
	}
	return pct, mount, &ts
}

// StaleHost es un host que el detector acaba de marcar como caido u offline.
type StaleHost struct {
	ID       string
	Hostname string
	// Status es el estado al que paso: "down" (grupo always_on) u "offline".
	Status string
}

// markStaleSQL pasa a down (grupo always_on) u offline (resto, incluidos los hosts sin grupo) los
// hosts up que no reportan hace mas que el umbral: down_after_seconds de su grupo o, si no tiene, el
// global ($1, en segundos). Los subplanes correlacionados re-evaluan last_seen_at bajo el lock de la
// fila (un CTE con join podria marcar un host que acaba de reportar). La transicion se registra en host_events
// en la misma sentencia.
const markStaleSQL = `
WITH upd AS (
  UPDATE hosts h
     SET status = CASE WHEN COALESCE((SELECT g.always_on FROM host_groups g WHERE g.id = h.group_id), false)
                       THEN 'down' ELSE 'offline' END,
         status_changed_at = now()
   WHERE h.status = 'up'
     AND h.last_seen_at < now() - make_interval(secs => COALESCE(
           (SELECT g.down_after_seconds FROM host_groups g WHERE g.id = h.group_id), $1::float8))
  RETURNING h.id, h.hostname, h.status
), ev AS (
  INSERT INTO host_events (host_id, from_status, to_status, source)
  SELECT id, 'up', status, 'detector' FROM upd
)
SELECT id, hostname, status FROM upd`

// MarkStaleHosts marca los hosts up que no reportan hace mas que su umbral (el de su grupo o
// globalDownAfter) y devuelve cuales cambio y a que estado. Es una sola sentencia: atomica e
// idempotente, segura con varias instancias (la segunda re-evalua el WHERE tras el lock y ya no ve
// el host como up). Usa el reloj de Postgres, el mismo que escribe last_seen_at. Nunca toca disabled
// ni pending, ni hosts que ya estan down u offline (su status_changed_at no se mueve).
func (s *Store) MarkStaleHosts(ctx context.Context, globalDownAfter time.Duration) ([]StaleHost, error) {
	rows, err := s.pool.Query(ctx, markStaleSQL, globalDownAfter.Seconds())
	if err != nil {
		return nil, fmt.Errorf("marcando hosts sin reportar: %w", err)
	}
	defer rows.Close()

	var stale []StaleHost
	for rows.Next() {
		var h StaleHost
		if err := rows.Scan(&h.ID, &h.Hostname, &h.Status); err != nil {
			return nil, fmt.Errorf("leyendo host sin reportar: %w", err)
		}
		stale = append(stale, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterando hosts sin reportar: %w", err)
	}
	return stale, nil
}
