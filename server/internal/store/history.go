package store

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

// avg acumula el promedio de una columna en un bucket; las muestras nulas no cuentan (igual que avg() de SQL).
type avg struct {
	sum float64
	n   int
}

func (a *avg) add(v float64) {
	a.sum += v
	a.n++
}

func (a *avg) addF32(p *float32) {
	if p != nil {
		a.add(float64(*p))
	}
}

func (a *avg) addI32(p *int32) {
	if p != nil {
		a.add(float64(*p))
	}
}

func (a *avg) ptr() *float64 {
	if a.n == 0 {
		return nil
	}
	v := a.sum / float64(a.n)
	return &v
}

// seriesBucket acumula las muestras de un bucket de DetailSeries.
type seriesBucket struct {
	cpu, memU, memT, swapU, swapT, load1, load5, load15, battery, rx, tx avg
}

// rateStep es el tramo entre una muestra y la anterior: los datos pa su tasa de red, que solo se decide al
// conocer la mediana de todos los tramos (mismo criterio de huecos que seriesSQL).
type rateStep struct {
	b            *seriesBucket
	secs         float64
	drx, dtx     int64
	hasRx, hasTx bool
}

// DetailSeries es MetricSeries pa un rango [from, to) leido de DetailRange (chunks + filas calientes): el detalle
// viejo ya no esta en SQL, asi que se agrupa aca. Misma semantica que seriesSQL: buckets de "step" alineados a la
// epoch, promedio por bucket, tasas de red con la diferencia entre muestras consecutivas atribuida al bucket de la
// muestra final, descartando reinicios de contador y tramos mayores que gapFactor x la mediana de los tramos del
// rango. Devuelve tambien el ts de la primera muestra (available_from) o nil si no hay ninguna.
// Memoria: una muestra por tramo (~40 B); el rango lo acota el llamador (31 dias = ~90 mil muestras).
func (s *Store) DetailSeries(ctx context.Context, hostID string, from, to time.Time, step time.Duration) ([]MetricPoint, *time.Time, error) {
	stepMicro := step.Microseconds()
	if stepMicro <= 0 {
		return nil, nil, fmt.Errorf("DetailSeries exige un paso positivo, got %s", step)
	}
	buckets := map[int64]*seriesBucket{}
	var steps []rateStep
	var prev *DetailSample
	var first *time.Time

	err := s.DetailRange(ctx, hostID, from, to, func(r DetailSample) error {
		key := r.TS.UnixMicro() / stepMicro
		b := buckets[key]
		if b == nil {
			b = &seriesBucket{}
			buckets[key] = b
		}
		b.cpu.addF32(r.CPUPct)
		b.memU.addI32(r.MemUsedMB)
		b.memT.addI32(r.MemTotalMB)
		b.swapU.addI32(r.SwapUsedMB)
		b.swapT.addI32(r.SwapTotalMB)
		b.load1.addF32(r.Load1)
		b.load5.addF32(r.Load5)
		b.load15.addF32(r.Load15)
		if r.BatteryPct != nil {
			b.battery.add(float64(*r.BatteryPct))
		}
		if prev == nil {
			ts := r.TS
			first = &ts
		} else {
			st := rateStep{b: b, secs: r.TS.Sub(prev.TS).Seconds()}
			if r.NetRxBytes != nil && prev.NetRxBytes != nil {
				st.drx, st.hasRx = *r.NetRxBytes-*prev.NetRxBytes, true
			}
			if r.NetTxBytes != nil && prev.NetTxBytes != nil {
				st.dtx, st.hasTx = *r.NetTxBytes-*prev.NetTxBytes, true
			}
			steps = append(steps, st)
		}
		cur := r
		prev = &cur
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	// Mediana de los tramos con tiempo positivo (percentile_cont(0.5) de seriesSQL). Sin tramos no hay tasas.
	gaps := make([]float64, 0, len(steps))
	for _, st := range steps {
		if st.secs > 0 {
			gaps = append(gaps, st.secs)
		}
	}
	if len(gaps) > 0 {
		maxGap := gapFactor * median(gaps)
		for _, st := range steps {
			if st.hasRx {
				if v := rate(st.drx, st.secs, maxGap); v != nil {
					st.b.rx.add(*v)
				}
			}
			if st.hasTx {
				if v := rate(st.dtx, st.secs, maxGap); v != nil {
					st.b.tx.add(*v)
				}
			}
		}
	}

	keys := make([]int64, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	points := make([]MetricPoint, 0, len(keys))
	for _, k := range keys {
		b := buckets[k]
		points = append(points, MetricPoint{
			TS:     time.UnixMicro(k * stepMicro).UTC(),
			CPUPct: b.cpu.ptr(), MemUsedMB: b.memU.ptr(), MemTotalMB: b.memT.ptr(),
			NetRxBps: b.rx.ptr(), NetTxBps: b.tx.ptr(),
			SwapUsedMB: b.swapU.ptr(), SwapTotalMB: b.swapT.ptr(),
			Load1: b.load1.ptr(), Load5: b.load5.ptr(), Load15: b.load15.ptr(),
			BatteryPct: b.battery.ptr(),
		})
	}
	return points, first, nil
}

// MetricSeriesHourlyRange es MetricSeriesHourly acotada a [from, to): los resumenes por hora cuya hora empieza
// en ese intervalo (el llamador alinea "from" a la hora pa incluir la que lo contiene). Devuelve tambien la
// menor hora existente en el rango (available_from), o nil si no hay resumenes.
func (s *Store) MetricSeriesHourlyRange(ctx context.Context, hostID string, from, to time.Time, step time.Duration) ([]MetricPoint, *time.Time, error) {
	points, err := s.queryPoints(ctx, "serie horaria de metricas", hourlySeriesSQL, hostID, from, step.Seconds(), to)
	if err != nil {
		return nil, nil, err
	}
	var first *time.Time
	if err := s.pool.QueryRow(ctx,
		`SELECT min(hour) FROM metrics_hourly WHERE host_id = $1 AND hour >= $2 AND hour < $3`, hostID, from, to).Scan(&first); err != nil {
		return nil, nil, fmt.Errorf("buscando la primera hora resumida: %w", err)
	}
	if first != nil {
		u := first.UTC()
		first = &u
	}
	return points, first, nil
}

// HourlyColumns son las columnas de valores de un resumen horario, en el orden de HourlyRow.Values.
var HourlyColumns = []string{
	"cpu_avg", "cpu_min", "cpu_max", "mem_used_avg_mb", "mem_used_max_mb", "mem_total_mb",
	"net_rx_bps_avg", "net_rx_bps_max", "net_tx_bps_avg", "net_tx_bps_max",
	"swap_used_avg_mb", "swap_used_max_mb", "swap_total_mb",
	"load1_avg", "load1_max", "load5_avg", "load15_avg", "battery_pct_avg", "battery_pct_min",
}

// HourlyRow es un resumen por hora; Values sigue el orden de HourlyColumns (nil = sin dato).
type HourlyRow struct {
	Hour    time.Time
	Samples int
	Values  []*float64
}

// HourlyRange entrega en orden los resumenes por hora con from <= hour < to, sin cargarlos todos en memoria.
func (s *Store) HourlyRange(ctx context.Context, hostID string, from, to time.Time, fn func(HourlyRow) error) error {
	cols := make([]string, len(HourlyColumns))
	for i, c := range HourlyColumns {
		cols[i] = c + "::float8" // nombres fijos de HourlyColumns: no vienen de la entrada
	}
	rows, err := s.pool.Query(ctx,
		`SELECT hour, samples, `+strings.Join(cols, ", ")+`
		   FROM metrics_hourly WHERE host_id = $1 AND hour >= $2 AND hour < $3 ORDER BY hour`, hostID, from, to)
	if err != nil {
		return fmt.Errorf("leyendo los resumenes por hora: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		r := HourlyRow{Values: make([]*float64, len(HourlyColumns))}
		dest := make([]any, 0, 2+len(HourlyColumns))
		dest = append(dest, &r.Hour, &r.Samples)
		for i := range r.Values {
			dest = append(dest, &r.Values[i])
		}
		if err := rows.Scan(dest...); err != nil {
			return fmt.Errorf("leyendo un resumen por hora: %w", err)
		}
		r.Hour = r.Hour.UTC()
		if err := fn(r); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterando los resumenes por hora: %w", err)
	}
	return nil
}

// DiskHistoryRow es una muestra historica de uso de disco de un montaje.
type DiskHistoryRow struct {
	TS      time.Time
	Mount   string
	UsedMB  int
	TotalMB int
}

// DiskSampleRange entrega en orden (ts, montaje) las muestras de disco con from <= ts < to, sin cargarlas todas.
// disk_samples solo conserva la retencion de disco (global, sin ventana caliente configurable ni por host/grupo):
// lo mas viejo simplemente no aparece. Mezcla crudo y disk_chunks empaquetados, igual que DetailRange con metrics.
func (s *Store) DiskSampleRange(ctx context.Context, hostID string, from, to time.Time, fn func(DiskHistoryRow) error) error {
	return s.DiskDetailRange(ctx, hostID, from, to, func(r DiskDetailSample) error {
		return fn(DiskHistoryRow{TS: r.TS, Mount: r.Mount, UsedMB: int(r.UsedMB), TotalMB: int(r.TotalMB)})
	})
}

// RateTracker calcula las tasas de red (bytes/s) de una secuencia de muestras en streaming, contra la muestra
// anterior: nil en la primera, tras un reinicio de contador o si el tramo pasa de MaxGap segundos.
type RateTracker struct {
	MaxGap float64
	prev   *DetailSample
}

// Next recibe la muestra siguiente y devuelve las tasas de rx y tx de su tramo.
func (t *RateTracker) Next(cur DetailSample) (rx, tx *float64) {
	prev := t.prev
	t.prev = &cur
	if prev == nil {
		return nil, nil
	}
	secs := cur.TS.Sub(prev.TS).Seconds()
	if cur.NetRxBytes != nil && prev.NetRxBytes != nil {
		rx = rate(*cur.NetRxBytes-*prev.NetRxBytes, secs, t.MaxGap)
	}
	if cur.NetTxBytes != nil && prev.NetTxBytes != nil {
		tx = rate(*cur.NetTxBytes-*prev.NetTxBytes, secs, t.MaxGap)
	}
	return rx, tx
}
