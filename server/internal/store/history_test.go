package store

import (
	"context"
	"testing"
	"time"

	"argos/server/internal/chunk"
)

func chI32(v int32) *int32     { return &v }
func chF32(v float32) *float32 { return &v }
func chI64(v int64) *int64     { return &v }
func chI16(v int16) *int16     { return &v }
func mustNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// insertFullRow inserta una fila de metrics con todas las columnas (nil = NULL).
func insertFullRow(t *testing.T, hostID string, ts time.Time, r chunk.Row) {
	t.Helper()
	mustExec(t,
		`INSERT INTO metrics (host_id, ts, cpu_pct, mem_used_mb, mem_total_mb, net_rx_bytes, net_tx_bytes,
		                      swap_used_mb, swap_total_mb, load1, load5, load15, battery_pct, battery_charging)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		hostID, ts, r.CPUPct, r.MemUsedMB, r.MemTotalMB, r.NetRxBytes, r.NetTxBytes,
		r.SwapUsedMB, r.SwapTotalMB, r.Load1, r.Load5, r.Load15, r.BatteryPct, r.BatteryCharging)
}

func samePoint(t *testing.T, i int, got, want MetricPoint) {
	t.Helper()
	if !got.TS.Equal(want.TS) {
		t.Errorf("punto %d: ts got %s, want %s", i, got.TS, want.TS)
	}
	for name, pair := range map[string][2]*float64{
		"cpu": {got.CPUPct, want.CPUPct}, "memU": {got.MemUsedMB, want.MemUsedMB}, "memT": {got.MemTotalMB, want.MemTotalMB},
		"rx": {got.NetRxBps, want.NetRxBps}, "tx": {got.NetTxBps, want.NetTxBps},
		"swapU": {got.SwapUsedMB, want.SwapUsedMB}, "swapT": {got.SwapTotalMB, want.SwapTotalMB},
		"load1": {got.Load1, want.Load1}, "load5": {got.Load5, want.Load5}, "load15": {got.Load15, want.Load15},
		"bat": {got.BatteryPct, want.BatteryPct},
	} {
		if !sameRate(pair[0], pair[1]) {
			t.Errorf("punto %d (%s) %s: got %s, want %s", i, got.TS.Format("15:04:05"), name, fmtRate(pair[0]), fmtRate(pair[1]))
		}
	}
}

// La agregacion en Go (DetailSeries) da lo mismo que seriesSQL sobre los mismos datos: promedios, tasas,
// reinicio de contador, hueco mayor que gapFactor x mediana y columnas nulas.
func TestDetailSeriesMatchesSQL(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	h := mkHost(t, "srv-go-vs-sql")

	ts := base
	rx, tx := int64(1000), int64(50)
	for i := 0; i < 60; i++ {
		switch i {
		case 20:
			rx = 100 // reinicio del contador de rx
		case 35:
			ts = ts.Add(10 * time.Minute) // hueco: el host no reporto
		}
		rx += 3000 + int64(i)*10
		tx += 300
		r := chunk.Row{CPUPct: chF32(float32(i%17) + 0.25), MemUsedMB: chI32(int32(1000 + i)), MemTotalMB: chI32(4096),
			NetRxBytes: chI64(rx), NetTxBytes: chI64(tx)}
		if i%3 == 0 { // columnas nuevas solo en algunas filas
			r.SwapUsedMB, r.SwapTotalMB = chI32(int32(i)), chI32(2048)
			r.Load1, r.Load5, r.Load15 = chF32(float32(i)/10), chF32(0.5), chF32(0.25)
			r.BatteryPct = chI16(int16(90 - i))
		}
		if i%7 == 0 { // contadores ausentes: sin tasa en esos tramos
			r.NetRxBytes, r.NetTxBytes = nil, nil
		}
		insertFullRow(t, h, ts, r)
		ts = ts.Add(30 * time.Second)
	}

	for _, step := range []time.Duration{30 * time.Second, time.Minute, 5 * time.Minute} {
		want, err := s.MetricSeries(ctx, h, base, step)
		mustNoErr(t, err)
		got, first, err := s.DetailSeries(ctx, h, base, base.Add(24*time.Hour), step)
		mustNoErr(t, err)
		if len(got) != len(want) || len(got) == 0 {
			t.Fatalf("paso %s: %d puntos, want %d (>0)", step, len(got), len(want))
		}
		for i := range want {
			samePoint(t, i, got[i], want[i])
		}
		if first == nil || !first.Equal(base) {
			t.Errorf("paso %s: primera muestra got %v, want %s", step, first, base)
		}
	}
}

// El limite superior "to" es exclusivo y una ventana sin datos da lista vacia y sin available_from.
func TestDetailSeriesBounds(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	h := mkHost(t, "srv-bounds")
	insertSamples(t, h,
		sample{secs: 0, cpu: 10, memU: 1, memT: 2}, sample{secs: 30, cpu: 20, memU: 1, memT: 2}, sample{secs: 60, cpu: 30, memU: 1, memT: 2})

	pts, first, err := s.DetailSeries(ctx, h, base, base.Add(60*time.Second), 30*time.Second)
	mustNoErr(t, err)
	if len(pts) != 2 || !near(pts[1].CPUPct, 20) {
		t.Errorf("to exclusivo: got %+v, want 2 puntos (0 y 30 s)", pts)
	}
	if first == nil || !first.Equal(base) {
		t.Errorf("available_from got %v, want %s", first, base)
	}
	pts, first, err = s.DetailSeries(ctx, h, base.Add(time.Hour), base.Add(2*time.Hour), time.Minute)
	mustNoErr(t, err)
	if len(pts) != 0 || first != nil {
		t.Errorf("ventana sin datos: got %d puntos, first %v; want vacio y nil", len(pts), first)
	}
}

// Un dia en medio de datos ya empaquetados (chunk, sin filas crudas) se sirve igual que el crudo.
func TestDetailSeriesReadsPackedDay(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	h := mkHost(t, "srv-chunk")
	day := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC) // un dia concreto ya empaquetado

	var rows []chunk.Row
	for i := 0; i < 2880; i++ { // un dia a 30 s
		rows = append(rows, chunk.Row{TS: day.Add(time.Duration(i) * 30 * time.Second), CPUPct: chF32(float32(i % 10)),
			MemUsedMB: chI32(500), MemTotalMB: chI32(1000), NetRxBytes: chI64(int64(i) * 3000), NetTxBytes: chI64(int64(i) * 300)})
	}
	blob, err := chunk.Encode(rows)
	mustNoErr(t, err)
	mustExec(t, `INSERT INTO metrics_chunks (host_id, day, n, codec, data) VALUES ($1, $2::date, $3, $4, $5)`,
		h, day, len(rows), chunk.Version, blob)

	from, to := day.Add(10*time.Hour), day.Add(12*time.Hour)
	pts, first, err := s.DetailSeries(ctx, h, from, to, 10*time.Minute)
	mustNoErr(t, err)
	if len(pts) != 12 {
		t.Fatalf("got %d puntos, want 12 (2 h a 10 min)", len(pts))
	}
	if !pts[0].TS.Equal(from) || !pts[11].TS.Equal(to.Add(-10*time.Minute)) {
		t.Errorf("rango de puntos got %s..%s, want %s..%s", pts[0].TS, pts[11].TS, from, to.Add(-10*time.Minute))
	}
	// 3000 B cada 30 s = 100 B/s (rx) y 10 B/s (tx) en todos los buckets; la primera muestra solo aporta su hora, no tasa.
	for i, p := range pts {
		if !near(p.NetRxBps, 100) || !near(p.NetTxBps, 10) {
			t.Errorf("punto %d: rx %s tx %s, want 100 y 10", i, fmtRate(p.NetRxBps), fmtRate(p.NetTxBps))
		}
	}
	if first == nil || !first.Equal(from) {
		t.Errorf("available_from got %v, want %s", first, from)
	}
}

func TestMetricSeriesHourlyRangeAndHourlyRange(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	h := mkHost(t, "srv-hourly-range")
	day := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		mustExec(t, `INSERT INTO metrics_hourly (host_id, hour, samples, cpu_avg, cpu_min, cpu_max, mem_used_avg_mb, mem_total_mb, load1_avg)
		             VALUES ($1, $2, 120, $3, 1, 99, 100, 200, 0.5)`, h, day.Add(time.Duration(i)*time.Hour), float64(10*(i+1)))
	}
	from, to := day.Add(time.Hour), day.Add(4*time.Hour) // horas 1, 2 y 3

	pts, first, err := s.MetricSeriesHourlyRange(ctx, h, from, to, time.Hour)
	mustNoErr(t, err)
	if len(pts) != 3 || !near(pts[0].CPUPct, 20) || !near(pts[2].CPUPct, 40) {
		t.Errorf("serie horaria got %d puntos %+v, want 3 (cpu 20..40)", len(pts), pts)
	}
	if first == nil || !first.Equal(from) {
		t.Errorf("available_from got %v, want %s", first, from)
	}

	var rows []HourlyRow
	mustNoErr(t, s.HourlyRange(ctx, h, from, to, func(r HourlyRow) error { rows = append(rows, r); return nil }))
	if len(rows) != 3 || rows[0].Samples != 120 || len(rows[0].Values) != len(HourlyColumns) {
		t.Fatalf("HourlyRange got %+v, want 3 filas de 120 muestras", rows)
	}
	if !near(rows[1].Values[0], 30) || !near(rows[1].Values[2], 99) || rows[1].Values[3] == nil || rows[1].Values[6] != nil {
		t.Errorf("valores de la fila 2: %+v (cpu_avg 30, cpu_max 99, mem 100, net nil)", rows[1].Values)
	}

	pts, first, err = s.MetricSeriesHourlyRange(ctx, h, day.AddDate(0, 0, 5), day.AddDate(0, 0, 6), time.Hour)
	mustNoErr(t, err)
	if len(pts) != 0 || first != nil {
		t.Errorf("sin resumenes: got %d puntos, first %v", len(pts), first)
	}
}

func TestDiskSampleRange(t *testing.T) {
	s := newTestStore(t)
	h := mkHost(t, "srv-disks")
	for i, m := range []string{"/", "/data", "/"} {
		mustExec(t, `INSERT INTO disk_samples (ts, host_id, used_mb, total_mb, mount) VALUES ($1, $2, 10, 100, $3)`,
			base.Add(time.Duration(i/2)*time.Minute), h, m)
	}
	var got []DiskHistoryRow
	mustNoErr(t, s.DiskSampleRange(context.Background(), h, base, base.Add(time.Minute), func(d DiskHistoryRow) error { got = append(got, d); return nil }))
	if len(got) != 2 || got[0].Mount != "/" || got[1].Mount != "/data" || got[0].UsedMB != 10 {
		t.Errorf("got %+v, want / y /data del primer minuto", got)
	}
}

func TestRateTracker(t *testing.T) {
	t.Parallel()
	mk := func(sec int, rx *int64) DetailSample {
		return DetailSample{TS: base.Add(time.Duration(sec) * time.Second), NetRxBytes: rx, NetTxBytes: chI64(1)}
	}
	tr := RateTracker{MaxGap: 600}
	steps := []struct {
		s    DetailSample
		want *float64
	}{
		{mk(0, chI64(1000)), nil},       // primera muestra
		{mk(30, chI64(4000)), ptr(100)}, // 3000 B / 30 s
		{mk(60, chI64(500)), nil},       // reinicio de contador
		{mk(90, chI64(3500)), ptr(100)}, // recupera
		{mk(1000, chI64(9000)), nil},    // hueco de 910 s > 600
		{mk(1030, nil), nil},            // contador ausente
	}
	for i, st := range steps {
		rx, _ := tr.Next(st.s)
		if !sameRate(rx, st.want) {
			t.Errorf("paso %d: rx got %s, want %s", i, fmtRate(rx), fmtRate(st.want))
		}
	}
}
