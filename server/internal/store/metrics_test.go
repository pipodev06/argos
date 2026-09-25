package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"
)

// base esta alineada a la epoch para todo step usado aca (10 s, 60 s, 1 h).
var base = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

func mkHost(t *testing.T, hostname string) string {
	t.Helper()
	var id string
	err := testPool.QueryRow(context.Background(),
		`INSERT INTO hosts (hostname, os, arch, agent_token_hash, status) VALUES ($1, 'linux', 'amd64', $2, 'up') RETURNING id`,
		hostname, th(hostname)).Scan(&id)
	if err != nil {
		t.Fatalf("creando host %q: %v", hostname, err)
	}
	return id
}

// sample es una fila de metrics; secs es el desplazamiento desde base.
type sample struct {
	secs   int
	cpu    float64
	memU   int
	memT   int
	rx, tx int64
}

func insertSamples(t *testing.T, hostID string, samples ...sample) {
	t.Helper()
	for _, s := range samples {
		mustExec(t,
			`INSERT INTO metrics (host_id, ts, cpu_pct, mem_used_mb, mem_total_mb, net_rx_bytes, net_tx_bytes)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			hostID, base.Add(time.Duration(s.secs)*time.Second), s.cpu, s.memU, s.memT, s.rx, s.tx)
	}
}

func near(p *float64, want float64) bool {
	return p != nil && math.Abs(*p-want) < 1e-6
}

func ptr(v float64) *float64 { return &v }

// sameRate compara dos tasas: ambas nil, o ambas con el mismo valor.
func sameRate(got, want *float64) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return math.Abs(*got-*want) < 1e-6
}

func fmtRate(p *float64) string {
	if p == nil {
		return "nil"
	}
	return fmt.Sprintf("%.3f", *p)
}

func TestMetricSeriesBucketsAndAverages(t *testing.T) {
	s := newTestStore(t)
	h := mkHost(t, "srv-buckets")
	// Dos muestras en el bucket 12:00, ninguna en 12:01 (hueco), una en 12:02.
	insertSamples(t, h,
		sample{secs: 10, cpu: 10, memU: 100, memT: 1000, rx: 0, tx: 0},
		sample{secs: 40, cpu: 20, memU: 200, memT: 1000, rx: 3000, tx: 300},
		sample{secs: 130, cpu: 50, memU: 300, memT: 1000, rx: 3000 + 9000, tx: 300 + 900},
	)

	pts, err := s.MetricSeries(context.Background(), h, base, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 2 {
		t.Fatalf("puntos got %d, want 2 (el bucket vacio no aparece): %+v", len(pts), pts)
	}
	if want := base; !pts[0].TS.Equal(want) || pts[0].TS.Location() != time.UTC {
		t.Errorf("ts[0] got %v, want %v en UTC", pts[0].TS, want)
	}
	if want := base.Add(2 * time.Minute); !pts[1].TS.Equal(want) {
		t.Errorf("ts[1] got %v, want %v", pts[1].TS, want)
	}
	if !near(pts[0].CPUPct, 15) || !near(pts[0].MemUsedMB, 150) || !near(pts[0].MemTotalMB, 1000) {
		t.Errorf("promedios del bucket 0 got cpu=%v memU=%v memT=%v, want 15/150/1000", pts[0].CPUPct, pts[0].MemUsedMB, pts[0].MemTotalMB)
	}
	// Bucket 0: la muestra de 10 s no tiene anterior (sin tasa); la de 40 s: 3000 B / 30 s = 100 B/s.
	if !near(pts[0].NetRxBps, 100) || !near(pts[0].NetTxBps, 10) {
		t.Errorf("tasa bucket 0 got rx=%v tx=%v, want 100/10", pts[0].NetRxBps, pts[0].NetTxBps)
	}
	// Bucket 2: 9000 B / 90 s = 100 B/s; 90 s <= 3x la mediana de los tramos (30 y 90 -> 60 s -> 180 s).
	if !near(pts[1].NetRxBps, 100) || !near(pts[1].NetTxBps, 10) {
		t.Errorf("tasa bucket 2 got rx=%v tx=%v, want 100/10", pts[1].NetRxBps, pts[1].NetTxBps)
	}
}

func TestMetricSeriesRateFromCumulativeCounters(t *testing.T) {
	s := newTestStore(t)
	h := mkHost(t, "srv-rate")
	// 10 s entre muestras, rx +1000 y tx +250 por muestra: 100 y 25 B/s. 12 muestras = 2 buckets de 60 s.
	var smp []sample
	for i := 0; i < 12; i++ {
		smp = append(smp, sample{secs: i * 10, cpu: 5, memU: 1, memT: 2, rx: int64(i) * 1000, tx: int64(i) * 250})
	}
	insertSamples(t, h, smp...)

	pts, err := s.MetricSeries(context.Background(), h, base, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 2 {
		t.Fatalf("puntos got %d, want 2", len(pts))
	}
	for i, p := range pts {
		if !near(p.NetRxBps, 100) || !near(p.NetTxBps, 25) {
			t.Errorf("punto %d got rx=%v tx=%v, want 100/25", i, p.NetRxBps, p.NetTxBps)
		}
	}
}

func TestMetricSeriesCounterReset(t *testing.T) {
	s := newTestStore(t)
	h := mkHost(t, "srv-reset")
	// step = 10 s: una muestra por bucket. La muestra de 30 s reinicia solo rx (50 < 3000);
	// tx sigue subiendo. Ese tramo se descarta (nil), nunca una tasa negativa ni un pico.
	insertSamples(t, h,
		sample{secs: 0, rx: 1000, tx: 0},
		sample{secs: 10, rx: 2000, tx: 250},
		sample{secs: 20, rx: 3000, tx: 500},
		sample{secs: 30, rx: 50, tx: 750},
		sample{secs: 40, rx: 1050, tx: 1000},
	)
	pts, err := s.MetricSeries(context.Background(), h, base, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 5 {
		t.Fatalf("puntos got %d, want 5", len(pts))
	}
	wantRx := []*float64{nil, ptr(100), ptr(100), nil, ptr(100)}
	wantTx := []*float64{nil, ptr(25), ptr(25), ptr(25), ptr(25)}
	for i, p := range pts {
		if !sameRate(p.NetRxBps, wantRx[i]) {
			t.Errorf("rx[%d] got %s, want %s", i, fmtRate(p.NetRxBps), fmtRate(wantRx[i]))
		}
		if !sameRate(p.NetTxBps, wantTx[i]) {
			t.Errorf("tx[%d] got %s, want %s", i, fmtRate(p.NetTxBps), fmtRate(wantTx[i]))
		}
		if p.NetRxBps != nil && *p.NetRxBps < 0 {
			t.Errorf("tasa negativa en el punto %d: %v", i, *p.NetRxBps)
		}
	}
}

func TestMetricSeriesLongGapDiscardsSpan(t *testing.T) {
	s := newTestStore(t)
	h := mkHost(t, "srv-gap")
	// Muestras cada 10 s, luego 10 min sin reportar (60 tramos > 3x10 s) y siguen.
	// El tramo del hueco se descarta aunque el contador solo suba; el resto conserva 100 B/s.
	insertSamples(t, h,
		sample{secs: 0, rx: 0},
		sample{secs: 10, rx: 1000},
		sample{secs: 20, rx: 2000},
		sample{secs: 30, rx: 3000},
		sample{secs: 630, rx: 9_999_000}, // tras el hueco de 600 s: tasa media seria 16 665 B/s (falso pico)
		sample{secs: 640, rx: 9_999_000 + 1000},
		sample{secs: 650, rx: 9_999_000 + 2000},
	)
	pts, err := s.MetricSeries(context.Background(), h, base, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 7 {
		t.Fatalf("puntos got %d, want 7 (los huecos entre buckets no se rellenan, pero hay una muestra por bucket)", len(pts))
	}
	want := []*float64{nil, ptr(100), ptr(100), ptr(100), nil, ptr(100), ptr(100)}
	for i, p := range pts {
		if !sameRate(p.NetRxBps, want[i]) {
			t.Errorf("rx[%d] got %s, want %s", i, fmtRate(p.NetRxBps), fmtRate(want[i]))
		}
	}
	// Los puntos son solo los buckets con datos: nada entre 30 s y 630 s.
	if got := pts[4].TS.Sub(pts[3].TS); got != 600*time.Second {
		t.Errorf("separacion entre puntos tras el hueco got %v, want 10m", got)
	}
}

func TestMetricSeriesEmptyAndUnknownHost(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	t.Run("host sin datos", func(t *testing.T) {
		h := mkHost(t, "srv-vacio")
		pts, err := s.MetricSeries(ctx, h, base, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if pts == nil || len(pts) != 0 {
			t.Errorf("puntos got %#v, want slice vacio no nulo", pts)
		}
	})
	t.Run("host inexistente", func(t *testing.T) {
		_, err := s.MetricSeries(ctx, "0f8fad5b-d9cb-469f-a165-70867728950e", base, time.Minute)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("err got %v, want ErrNotFound", err)
		}
	})
	t.Run("datos solo fuera del rango", func(t *testing.T) {
		h := mkHost(t, "srv-viejo")
		insertSamples(t, h, sample{secs: -3600, cpu: 1})
		pts, err := s.MetricSeries(ctx, h, base, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if len(pts) != 0 {
			t.Errorf("puntos got %d, want 0", len(pts))
		}
	})
}

func TestMetricSeriesSingleSample(t *testing.T) {
	s := newTestStore(t)
	h := mkHost(t, "srv-uno")
	insertSamples(t, h, sample{secs: 5, cpu: 42, memU: 7, memT: 8, rx: 500, tx: 600})
	pts, err := s.MetricSeries(context.Background(), h, base, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 1 {
		t.Fatalf("puntos got %d, want 1", len(pts))
	}
	p := pts[0]
	if !near(p.CPUPct, 42) || !near(p.MemUsedMB, 7) || !near(p.MemTotalMB, 8) {
		t.Errorf("punto got %+v, want cpu 42 mem 7/8", p)
	}
	if p.NetRxBps != nil || p.NetTxBps != nil {
		t.Errorf("con una sola muestra la tasa debe ser nil, got rx=%s tx=%s", fmtRate(p.NetRxBps), fmtRate(p.NetTxBps))
	}
}

func TestMetricSeriesNullColumnsAndIsolationBetweenHosts(t *testing.T) {
	s := newTestStore(t)
	a, b := mkHost(t, "srv-a"), mkHost(t, "srv-b")
	mustExec(t, `INSERT INTO metrics (host_id, ts) VALUES ($1, $2), ($1, $3)`, a, base.Add(time.Second), base.Add(11*time.Second))
	insertSamples(t, b, sample{secs: 1, cpu: 99, rx: 1e9})

	pts, err := s.MetricSeries(context.Background(), a, base, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 1 {
		t.Fatalf("puntos got %d, want 1", len(pts))
	}
	if p := pts[0]; p.CPUPct != nil || p.MemUsedMB != nil || p.NetRxBps != nil || p.NetTxBps != nil {
		t.Errorf("columnas nulas deben dar nil, got %+v", p)
	}
}

func TestMetricSeriesBoundedByBuckets(t *testing.T) {
	s := newTestStore(t)
	h := mkHost(t, "srv-acotado")
	// 2 h de datos cada 10 s con step de 30 s: como mucho 2h/30s = 240 (+1) puntos, no 720 filas.
	mustExec(t,
		`INSERT INTO metrics (host_id, ts, cpu_pct, mem_used_mb, mem_total_mb, net_rx_bytes, net_tx_bytes)
		 SELECT $1, $2::timestamptz + g * interval '10 seconds', 1, 1, 1, g * 100, g * 100
		   FROM generate_series(0, 719) g`, h, base)
	pts, err := s.MetricSeries(context.Background(), h, base, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 240 {
		t.Errorf("puntos got %d, want 240", len(pts))
	}
	for i := 1; i < len(pts); i++ {
		if !pts[i].TS.After(pts[i-1].TS) {
			t.Fatalf("puntos no ascendentes en %d: %v <= %v", i, pts[i].TS, pts[i-1].TS)
		}
	}
}

func TestLatestMetrics(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	t.Run("sin metricas", func(t *testing.T) {
		h := mkHost(t, "srv-l0")
		got, err := s.LatestMetrics(ctx, h)
		if err != nil || got != nil {
			t.Errorf("got %+v, %v; want nil, nil", got, err)
		}
	})
	t.Run("una muestra: net nil", func(t *testing.T) {
		h := mkHost(t, "srv-l1")
		insertSamples(t, h, sample{secs: 0, cpu: 33, memU: 5, memT: 9, rx: 100, tx: 200})
		got, err := s.LatestMetrics(ctx, h)
		if err != nil || got == nil {
			t.Fatalf("got %+v, %v", got, err)
		}
		if !near(got.CPUPct, 33) || !near(got.MemUsedMB, 5) || !near(got.MemTotalMB, 9) || got.NetRxBps != nil || got.NetTxBps != nil {
			t.Errorf("got %+v, want cpu 33 mem 5/9 y net nil", got)
		}
	})
	t.Run("dos muestras: tasa contra la anterior", func(t *testing.T) {
		h := mkHost(t, "srv-l2")
		insertSamples(t, h,
			sample{secs: 0, cpu: 1, rx: 0, tx: 0},
			sample{secs: 20, cpu: 2, rx: 5000, tx: 1000},
			sample{secs: 40, cpu: 3, rx: 9000, tx: 2000},
		)
		got, err := s.LatestMetrics(ctx, h)
		if err != nil || got == nil {
			t.Fatalf("got %+v, %v", got, err)
		}
		if !got.TS.Equal(base.Add(40*time.Second)) || !near(got.CPUPct, 3) {
			t.Errorf("no es la ultima muestra: %+v", got)
		}
		if !near(got.NetRxBps, 200) || !near(got.NetTxBps, 50) {
			t.Errorf("tasa got rx=%s tx=%s, want 200/50", fmtRate(got.NetRxBps), fmtRate(got.NetTxBps))
		}
	})
	t.Run("reinicio de contador: nil", func(t *testing.T) {
		h := mkHost(t, "srv-l3")
		insertSamples(t, h,
			sample{secs: 0, rx: 9000, tx: 100},
			sample{secs: 10, rx: 10, tx: 200},
		)
		got, err := s.LatestMetrics(ctx, h)
		if err != nil || got == nil {
			t.Fatalf("got %+v, %v", got, err)
		}
		if got.NetRxBps != nil || !near(got.NetTxBps, 10) {
			t.Errorf("got rx=%s tx=%s, want nil/10", fmtRate(got.NetRxBps), fmtRate(got.NetTxBps))
		}
	})
	t.Run("hueco largo antes de la ultima: nil", func(t *testing.T) {
		h := mkHost(t, "srv-l4")
		insertSamples(t, h,
			sample{secs: 0, rx: 0},
			sample{secs: 10, rx: 1000},
			sample{secs: 20, rx: 2000},
			sample{secs: 30, rx: 3000},
			sample{secs: 3630, rx: 4000}, // vuelve tras 1 h sin reportar
		)
		got, err := s.LatestMetrics(ctx, h)
		if err != nil || got == nil {
			t.Fatalf("got %+v, %v", got, err)
		}
		if got.NetRxBps != nil {
			t.Errorf("rx got %s, want nil (hueco)", fmtRate(got.NetRxBps))
		}
	})
}

func TestHostByID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	h := mkHost(t, "srv-ficha")

	got, err := s.HostByID(ctx, AllScope(), h)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != h || got.Hostname != "srv-ficha" || got.OS != "linux" || got.Arch != "amd64" || got.Status != "up" || got.CreatedAt.IsZero() {
		t.Errorf("got %+v", got)
	}

	mustExec(t, `UPDATE hosts SET os = NULL, arch = NULL WHERE id = $1`, h)
	got, err = s.HostByID(ctx, AllScope(), h)
	if err != nil {
		t.Fatal(err)
	}
	if got.OS != "" || got.Arch != "" {
		t.Errorf("os/arch nulos deben leerse como vacio, got %q/%q", got.OS, got.Arch)
	}

	if _, err := s.HostByID(ctx, AllScope(), "0f8fad5b-d9cb-469f-a165-70867728950e"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err got %v, want ErrNotFound", err)
	}
}

func TestMedianAndRate(t *testing.T) {
	if got := median([]float64{30, 10, 20}); got != 20 {
		t.Errorf("mediana impar got %v, want 20", got)
	}
	if got := median([]float64{10, 20, 30, 40}); got != 25 {
		t.Errorf("mediana par got %v, want 25", got)
	}
	tests := []struct {
		name      string
		delta     int64
		secs, max float64
		want      *float64
	}{
		{"normal", 1000, 10, 30, ptr(100)},
		{"delta cero", 0, 10, 30, ptr(0)},
		{"negativo", -1, 10, 30, nil},
		{"tiempo cero", 5, 0, 30, nil},
		{"hueco", 5, 31, 30, nil},
		{"justo en el limite", 300, 30, 30, ptr(10)},
	}
	for _, tt := range tests {
		if got := rate(tt.delta, tt.secs, tt.max); !sameRate(got, tt.want) {
			t.Errorf("%s: got %s, want %s", tt.name, fmtRate(got), fmtRate(tt.want))
		}
	}
}
