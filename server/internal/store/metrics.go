package store

import (
	"context"
	"fmt"
	"slices"
	"time"
)

// gapFactor: un tramo entre dos muestras consecutivas mas largo que gapFactor veces el
// intervalo tipico (mediana de los tramos) es un hueco (host sin reportar): no se usa
// pa calcular tasas de red, porque promediaria el trafico de un apagon completo.
const gapFactor = 3

// latestWindow es cuantas muestras recientes se leen pa fijar el intervalo tipico de
// "latest". Con menos de 2 no hay tasa.
const latestWindow = 20

// MetricPoint es una muestra (o el promedio de un bucket) de metricas de un host.
// Un campo nil es "sin dato": columna nula, o tasa de red no calculable (primera
// muestra, reinicio de contador o hueco).
type MetricPoint struct {
	TS         time.Time
	CPUPct     *float64
	MemUsedMB  *float64
	MemTotalMB *float64
	NetRxBps   *float64 // bytes por segundo
	NetTxBps   *float64

	// Swap, carga y bateria: nil = sin dato (agente viejo, Windows sin load average, equipo sin bateria).
	// En una serie son el promedio del bucket; en latest, el ultimo valor.
	SwapUsedMB      *float64
	SwapTotalMB     *float64
	Load1           *float64
	Load5           *float64
	Load15          *float64
	BatteryPct      *float64
	BatteryCharging *bool // solo en latest
}

// LatestMetrics devuelve la ultima muestra del host con las tasas de red calculadas
// contra la muestra anterior. Devuelve nil (sin error) si el host no tiene metricas.
// Las tasas son nil con una sola muestra, tras un reinicio de contador o si el tramo
// es un hueco respecto al intervalo tipico de las ultimas muestras.
func (s *Store) LatestMetrics(ctx context.Context, hostID string) (*MetricPoint, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT ts, cpu_pct, mem_used_mb, mem_total_mb, net_rx_bytes, net_tx_bytes,
		        swap_used_mb, swap_total_mb, load1, load5, load15, battery_pct, battery_charging
		   FROM metrics WHERE host_id = $1 ORDER BY ts DESC LIMIT $2`, hostID, latestWindow)
	if err != nil {
		return nil, fmt.Errorf("leyendo ultimas metricas: %w", err)
	}
	defer rows.Close()

	type raw struct {
		ts              time.Time
		cpu             *float32
		memU            *int32
		memT            *int32
		rx, tx          *int64
		swapU, swapT    *int32
		load1, load5    *float32
		load15          *float32
		batteryPct      *int16
		batteryCharging *bool
	}
	var samples []raw // mas nueva primero
	for rows.Next() {
		var r raw
		if err := rows.Scan(&r.ts, &r.cpu, &r.memU, &r.memT, &r.rx, &r.tx,
			&r.swapU, &r.swapT, &r.load1, &r.load5, &r.load15, &r.batteryPct, &r.batteryCharging); err != nil {
			return nil, fmt.Errorf("leyendo muestra: %w", err)
		}
		samples = append(samples, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterando muestras: %w", err)
	}
	if len(samples) == 0 {
		return nil, nil
	}

	cur := samples[0]
	p := &MetricPoint{TS: cur.ts.UTC()}
	if cur.cpu != nil {
		v := float64(*cur.cpu)
		p.CPUPct = &v
	}
	p.MemUsedMB = intPtrToFloat(cur.memU)
	p.MemTotalMB = intPtrToFloat(cur.memT)
	p.SwapUsedMB = intPtrToFloat(cur.swapU)
	p.SwapTotalMB = intPtrToFloat(cur.swapT)
	p.Load1 = float32PtrToFloat(cur.load1)
	p.Load5 = float32PtrToFloat(cur.load5)
	p.Load15 = float32PtrToFloat(cur.load15)
	if cur.batteryPct != nil {
		v := float64(*cur.batteryPct)
		p.BatteryPct = &v
	}
	p.BatteryCharging = cur.batteryCharging

	if len(samples) < 2 {
		return p, nil
	}
	gaps := make([]float64, 0, len(samples)-1)
	for i := 0; i+1 < len(samples); i++ {
		gaps = append(gaps, samples[i].ts.Sub(samples[i+1].ts).Seconds())
	}
	maxGap := gapFactor * median(gaps)
	secs := gaps[0]
	prev := samples[1]
	if cur.rx != nil && prev.rx != nil {
		p.NetRxBps = rate(*cur.rx-*prev.rx, secs, maxGap)
	}
	if cur.tx != nil && prev.tx != nil {
		p.NetTxBps = rate(*cur.tx-*prev.tx, secs, maxGap)
	}
	return p, nil
}

func float32PtrToFloat(v *float32) *float64 {
	if v == nil {
		return nil
	}
	f := float64(*v)
	return &f
}

func intPtrToFloat(v *int32) *float64 {
	if v == nil {
		return nil
	}
	f := float64(*v)
	return &f
}

// rate es delta/secs en bytes por segundo, o nil si el tramo no es fiable: contador
// reiniciado (delta negativo), tiempo no positivo o hueco mayor que maxGap.
func rate(delta int64, secs, maxGap float64) *float64 {
	if delta < 0 || secs <= 0 || secs > maxGap {
		return nil
	}
	r := float64(delta) / secs
	return &r
}

// median es la mediana con interpolacion (igual que percentile_cont(0.5) de Postgres).
func median(v []float64) float64 {
	s := slices.Clone(v)
	slices.Sort(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// seriesSQL agrupa las muestras de un host desde $2 en buckets de $3 segundos
// (date_bin alineado a la epoch). Un solo barrido del indice PK (host_id, ts): la
// ventana LAG da los deltas de los contadores acumulados; el tramo de una muestra se
// atribuye al bucket de su extremo final. Se descartan (NULL) los tramos con delta
// negativo (reinicio de contador) o mas largos que gapFactor x la mediana de los
// tramos de la ventana (huecos). La salida esta acotada por el numero de buckets.
const seriesSQL = `
WITH s AS (
  SELECT ts, cpu_pct, mem_used_mb, mem_total_mb,
         swap_used_mb, swap_total_mb, load1, load5, load15, battery_pct,
         EXTRACT(EPOCH FROM ts - lag(ts) OVER w)::float8 AS secs,
         net_rx_bytes - lag(net_rx_bytes) OVER w AS drx,
         net_tx_bytes - lag(net_tx_bytes) OVER w AS dtx
    FROM metrics
   WHERE host_id = $1 AND ts >= $2
  WINDOW w AS (ORDER BY ts)
), lim AS (
  SELECT ($4::float8 * percentile_cont(0.5) WITHIN GROUP (ORDER BY secs)) AS max_gap
    FROM s WHERE secs > 0
)
SELECT date_bin(make_interval(secs => $3::float8), s.ts, TIMESTAMPTZ 'epoch') AS bucket,
       avg(cpu_pct)::float8,
       avg(mem_used_mb)::float8,
       avg(mem_total_mb)::float8,
       avg(CASE WHEN drx >= 0 AND secs > 0 AND secs <= max_gap THEN drx::float8 / secs END),
       avg(CASE WHEN dtx >= 0 AND secs > 0 AND secs <= max_gap THEN dtx::float8 / secs END),
       avg(swap_used_mb)::float8,
       avg(swap_total_mb)::float8,
       avg(load1)::float8,
       avg(load5)::float8,
       avg(load15)::float8,
       avg(battery_pct)::float8
  FROM s CROSS JOIN lim
 GROUP BY bucket
 ORDER BY bucket`

// MetricSeries devuelve las metricas del host desde "from" agrupadas en buckets de
// "step", en orden ascendente y solo los buckets con datos. Devuelve ErrNotFound si
// no hay puntos y el host no existe.
func (s *Store) MetricSeries(ctx context.Context, hostID string, from time.Time, step time.Duration) ([]MetricPoint, error) {
	return s.series(ctx, hostID, seriesSQL, "serie de metricas", hostID, from, step.Seconds(), float64(gapFactor))
}

// hourlySeriesSQL agrupa los resumenes por hora de un host desde $2 en buckets de $3 segundos
// (date_bin alineado a la epoch, multiplos de 1 h). Cada resumen pesa por sus muestras (samples),
// asi el promedio del bucket es el promedio de las muestras crudas que resume; una columna nula
// (sin dato en toda la hora) no cuenta ni en el numerador ni en el peso. Solo hay filas de horas
// ya cerradas: la hora en curso aun no esta resumida.
const hourlySeriesSQL = `
SELECT date_bin(make_interval(secs => $3::float8), hour, TIMESTAMPTZ 'epoch') AS bucket,
       sum(cpu_avg::float8 * samples) / nullif(sum(samples) FILTER (WHERE cpu_avg IS NOT NULL), 0),
       sum(mem_used_avg_mb::float8 * samples) / nullif(sum(samples) FILTER (WHERE mem_used_avg_mb IS NOT NULL), 0),
       sum(mem_total_mb::float8 * samples) / nullif(sum(samples) FILTER (WHERE mem_total_mb IS NOT NULL), 0),
       sum(net_rx_bps_avg::float8 * samples) / nullif(sum(samples) FILTER (WHERE net_rx_bps_avg IS NOT NULL), 0),
       sum(net_tx_bps_avg::float8 * samples) / nullif(sum(samples) FILTER (WHERE net_tx_bps_avg IS NOT NULL), 0),
       sum(swap_used_avg_mb::float8 * samples) / nullif(sum(samples) FILTER (WHERE swap_used_avg_mb IS NOT NULL), 0),
       sum(swap_total_mb::float8 * samples) / nullif(sum(samples) FILTER (WHERE swap_total_mb IS NOT NULL), 0),
       sum(load1_avg::float8 * samples) / nullif(sum(samples) FILTER (WHERE load1_avg IS NOT NULL), 0),
       sum(load5_avg::float8 * samples) / nullif(sum(samples) FILTER (WHERE load5_avg IS NOT NULL), 0),
       sum(load15_avg::float8 * samples) / nullif(sum(samples) FILTER (WHERE load15_avg IS NOT NULL), 0),
       sum(battery_pct_avg::float8 * samples) / nullif(sum(samples) FILTER (WHERE battery_pct_avg IS NOT NULL), 0)
  FROM metrics_hourly
 WHERE host_id = $1 AND hour >= $2
 GROUP BY bucket
 ORDER BY bucket`

// MetricSeriesHourly es MetricSeries pero leyendo los resumenes por hora (metrics_hourly), pa
// los rangos largos (30d, 90d, 1a) cuyo crudo ya no se conserva o seria carisimo de agrupar.
// step debe ser multiplo de 1 h. Mismo contrato de puntos y de ErrNotFound.
func (s *Store) MetricSeriesHourly(ctx context.Context, hostID string, from time.Time, step time.Duration) ([]MetricPoint, error) {
	return s.series(ctx, hostID, hourlySeriesSQL, "serie horaria de metricas", hostID, from, step.Seconds())
}

// series ejecuta una consulta de serie (columnas: bucket, cpu, mem usada, mem total, rx, tx, swap usado,
// swap total, load1, load5, load15, bateria) y
// arma los puntos. Sin puntos, distingue "host sin datos en el rango" de "host inexistente".
func (s *Store) series(ctx context.Context, hostID, query, what string, args ...any) ([]MetricPoint, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("consultando %s: %w", what, err)
	}
	defer rows.Close()

	points := []MetricPoint{}
	for rows.Next() {
		var p MetricPoint
		if err := rows.Scan(&p.TS, &p.CPUPct, &p.MemUsedMB, &p.MemTotalMB, &p.NetRxBps, &p.NetTxBps,
			&p.SwapUsedMB, &p.SwapTotalMB, &p.Load1, &p.Load5, &p.Load15, &p.BatteryPct); err != nil {
			return nil, fmt.Errorf("leyendo punto de la %s: %w", what, err)
		}
		p.TS = p.TS.UTC()
		points = append(points, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterando la %s: %w", what, err)
	}
	rows.Close()

	if len(points) == 0 {
		var exists bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM hosts WHERE id = $1)`, hostID).Scan(&exists); err != nil {
			return nil, fmt.Errorf("comprobando host: %w", err)
		}
		if !exists {
			return nil, ErrNotFound
		}
	}
	return points, nil
}
