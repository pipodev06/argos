package retention

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"argos/server/internal/store"
)

// h0 es una hora alineada a UTC en el pasado (cae en la particion legacy) pa los datos de los tests.
var h0 = time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

// hourlyRow es una fila leida de metrics_hourly.
type hourlyRow struct {
	found                    bool
	samples                  int
	cpuAvg, cpuMin, cpuMax   *float64
	memAvg, memMax, memTotal *int
	rxAvg, rxMax             *float64
	txAvg, txMax             *float64
	xmin                     string
}

func readHourly(t *testing.T, hostID string, hour time.Time) hourlyRow {
	t.Helper()
	var r hourlyRow
	var cpuAvg, cpuMin, cpuMax, rxAvg, rxMax, txAvg, txMax *float32
	err := testPool.QueryRow(context.Background(),
		`SELECT samples, cpu_avg, cpu_min, cpu_max, mem_used_avg_mb, mem_used_max_mb, mem_total_mb,
		        net_rx_bps_avg, net_rx_bps_max, net_tx_bps_avg, net_tx_bps_max, xmin::text
		   FROM metrics_hourly WHERE host_id = $1 AND hour = $2`, hostID, hour,
	).Scan(&r.samples, &cpuAvg, &cpuMin, &cpuMax, &r.memAvg, &r.memMax, &r.memTotal, &rxAvg, &rxMax, &txAvg, &txMax, &r.xmin)
	if errors.Is(err, pgx.ErrNoRows) {
		return r
	}
	if err != nil {
		t.Fatalf("leyendo metrics_hourly: %v", err)
	}
	r.found = true
	conv := func(v *float32) *float64 {
		if v == nil {
			return nil
		}
		f := float64(*v)
		return &f
	}
	r.cpuAvg, r.cpuMin, r.cpuMax = conv(cpuAvg), conv(cpuMin), conv(cpuMax)
	r.rxAvg, r.rxMax, r.txAvg, r.txMax = conv(rxAvg), conv(rxMax), conv(txAvg), conv(txMax)
	return r
}

func near(p *float64, want float64) bool {
	return p != nil && math.Abs(*p-want) < 1e-3
}

func show(p *float64) string {
	if p == nil {
		return "NULL"
	}
	return fmt.Sprintf("%.3f", *p)
}

// minuteSeries genera n muestras, una por minuto desde start (i = 0..n-1), salvo las que skip descarte.
func minuteSeries(start time.Time, n int, skip func(i int) bool,
	cpu func(i int) float64, mem func(i int) int, rx, tx func(i int) int64) []sampleRow {
	var rows []sampleRow
	for i := range n {
		if skip != nil && skip(i) {
			continue
		}
		r := sampleRow{ts: start.Add(time.Duration(i) * time.Minute), memT: ip(4000)}
		if cpu != nil {
			r.cpu = f64(cpu(i))
		}
		if mem != nil {
			r.memU = ip(mem(i))
		}
		if rx != nil {
			r.rx = i64(rx(i))
		}
		if tx != nil {
			r.tx = i64(tx(i))
		}
		rows = append(rows, r)
	}
	return rows
}

func runRollup(t *testing.T, m *Maintainer, now time.Time) {
	t.Helper()
	withConn(t, func(conn *pgx.Conn) {
		if err := m.rollup(context.Background(), conn, now); err != nil {
			t.Fatalf("rollup: %v", err)
		}
	})
}

func throughOf(t *testing.T) time.Time {
	t.Helper()
	var v time.Time
	if err := testPool.QueryRow(context.Background(),
		`SELECT value_ts FROM maintenance_state WHERE name = $1`, rollupMarker).Scan(&v); err != nil {
		t.Fatalf("leyendo la marca de agua: %v", err)
	}
	return v.UTC()
}

func linear(step int64) func(i int) int64 { return func(i int) int64 { return int64(i) * step } }

func TestRollupAveragesAndRates(t *testing.T) {
	cpu := func(i int) float64 { return float64(i) }
	mem := func(i int) int { return 1000 + i }

	tests := []struct {
		name       string
		rows       []sampleRow
		samples    int
		wantRx     *float64 // nil = NULL
		wantRxMax  *float64
		wantTx     *float64
		wantTxMax  *float64
		wantCPUAvg float64
	}{
		{
			// 60 muestras a 60 s; rx +6000 por tramo = 100 B/s, tx +3000 = 50 B/s. La primera muestra no tiene tramo previo.
			name:    "promedios y tasas constantes",
			rows:    minuteSeries(h0, 60, nil, cpu, mem, linear(6000), linear(3000)),
			samples: 60, wantRx: f64(100), wantRxMax: f64(100), wantTx: f64(50), wantTxMax: f64(50), wantCPUAvg: 29.5,
		},
		{
			// El contador rx se reinicia en i=30 y luego sube a 300 B/s: el tramo del reinicio no cuenta.
			// (29 tramos a 100 + 29 tramos a 300) -> promedio ponderado por tiempo 200, maximo 300.
			name: "reinicio de contador",
			rows: minuteSeries(h0, 60, nil, cpu, mem, func(i int) int64 {
				if i < 30 {
					return int64(i) * 6000
				}
				return int64(i-30) * 18000
			}, linear(3000)),
			samples: 60, wantRx: f64(200), wantRxMax: f64(300), wantTx: f64(50), wantTxMax: f64(50), wantCPUAvg: 29.5,
		},
		{
			// Sin muestras entre i=20 y i=39: el tramo de 21 min supera 3x la mediana (60 s) y no cuenta.
			// cpu medio del hueco: muestras 0..19 y 40..59 = (190 + 990) / 40 = 29.5.
			name:    "hueco de 20 minutos",
			rows:    minuteSeries(h0, 60, func(i int) bool { return i >= 20 && i < 40 }, cpu, mem, linear(6000), linear(3000)),
			samples: 40, wantRx: f64(100), wantRxMax: f64(100), wantTx: f64(50), wantTxMax: f64(50), wantCPUAvg: 29.5,
		},
		{
			name:    "contadores nulos",
			rows:    minuteSeries(h0, 60, nil, cpu, mem, nil, nil),
			samples: 60, wantCPUAvg: 29.5,
		},
		{
			name:    "una sola muestra",
			rows:    minuteSeries(h0, 1, nil, cpu, mem, linear(6000), linear(3000)),
			samples: 1, wantCPUAvg: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fresh(t)
			h := mkHost(t, "srv-rollup")
			insertRows(t, h, tt.rows)
			m, logs := newMaint(t, testCfg, h0.Add(5*time.Hour+30*time.Minute))
			runRollup(t, m, m.now())

			r := readHourly(t, h, h0)
			if !r.found {
				t.Fatalf("no se genero el resumen de la hora")
			}
			if r.samples != tt.samples {
				t.Errorf("samples got %d, want %d", r.samples, tt.samples)
			}
			if !near(r.cpuAvg, tt.wantCPUAvg) {
				t.Errorf("cpu_avg got %s, want %.3f", show(r.cpuAvg), tt.wantCPUAvg)
			}
			for _, c := range []struct {
				name      string
				got, want *float64
			}{
				{"net_rx_bps_avg", r.rxAvg, tt.wantRx}, {"net_rx_bps_max", r.rxMax, tt.wantRxMax},
				{"net_tx_bps_avg", r.txAvg, tt.wantTx}, {"net_tx_bps_max", r.txMax, tt.wantTxMax},
			} {
				if c.want == nil {
					if c.got != nil {
						t.Errorf("%s got %s, want NULL", c.name, show(c.got))
					}
				} else if !near(c.got, *c.want) {
					t.Errorf("%s got %s, want %.3f", c.name, show(c.got), *c.want)
				}
			}
			logs.noErrors(t)
		})
	}
}

func TestRollupHourColumns(t *testing.T) {
	fresh(t)
	h := mkHost(t, "srv-cols")
	insertRows(t, h, minuteSeries(h0, 60, nil,
		func(i int) float64 { return float64(i) }, func(i int) int { return 1000 + i }, linear(6000), linear(3000)))
	m, _ := newMaint(t, testCfg, h0.Add(2*time.Hour))
	runRollup(t, m, m.now())

	r := readHourly(t, h, h0)
	if !near(r.cpuMin, 0) || !near(r.cpuMax, 59) {
		t.Errorf("cpu min/max got %s/%s, want 0/59", show(r.cpuMin), show(r.cpuMax))
	}
	// avg(1000..1059) = 1029.5 -> redondea a 1030; max 1059; total 4000.
	if r.memAvg == nil || *r.memAvg != 1030 || r.memMax == nil || *r.memMax != 1059 || r.memTotal == nil || *r.memTotal != 4000 {
		t.Errorf("mem avg/max/total got %v/%v/%v, want 1030/1059/4000", r.memAvg, r.memMax, r.memTotal)
	}
}

// El primer tramo de una hora se calcula contra la ultima muestra de la hora anterior (lookback de 15 min).
func TestRollupRateAcrossHourBoundary(t *testing.T) {
	fresh(t)
	h := mkHost(t, "srv-boundary")
	insertRows(t, h, minuteSeries(h0, 120, nil, func(i int) float64 { return 10 }, func(i int) int { return 100 }, linear(6000), nil))
	m, logs := newMaint(t, testCfg, h0.Add(4*time.Hour))
	runRollup(t, m, m.now())

	first, second := readHourly(t, h, h0), readHourly(t, h, h0.Add(time.Hour))
	if first.samples != 60 || second.samples != 60 {
		t.Fatalf("samples got %d/%d, want 60/60", first.samples, second.samples)
	}
	// La hora 10 pierde el tramo de la primera muestra (59 tramos); la 11 tiene los 60 (incluido el que cruza la hora).
	if !near(first.rxAvg, 100) || !near(second.rxAvg, 100) {
		t.Errorf("rx avg got %s/%s, want 100/100", show(first.rxAvg), show(second.rxAvg))
	}
	logs.noErrors(t)
}

func TestRollupSkipsHoursWithoutData(t *testing.T) {
	fresh(t)
	h := mkHost(t, "srv-huecos")
	insertRows(t, h, minuteSeries(h0, 30, nil, func(int) float64 { return 1 }, nil, nil, nil))
	insertRows(t, h, minuteSeries(h0.Add(2*time.Hour), 30, nil, func(int) float64 { return 1 }, nil, nil, nil))
	m, _ := newMaint(t, testCfg, h0.Add(5*time.Hour+30*time.Minute))
	runRollup(t, m, m.now())

	if got := count(t, `SELECT count(*) FROM metrics_hourly WHERE host_id = $1`, h); got != 2 {
		t.Errorf("resumenes got %d, want 2 (la hora sin datos no genera fila)", got)
	}
	if readHourly(t, h, h0.Add(time.Hour)).found {
		t.Errorf("hay resumen de la hora sin datos")
	}
	if got, want := throughOf(t), h0.Add(5*time.Hour); !got.Equal(want) {
		t.Errorf("marca de agua got %v, want %v (la hora en curso no se resume)", got, want)
	}
}

func TestRollupIsIdempotent(t *testing.T) {
	fresh(t)
	h := mkHost(t, "srv-idem")
	rows := minuteSeries(h0, 180, nil, func(i int) float64 { return float64(i % 50) }, func(i int) int { return 500 + i }, linear(1000), linear(500))
	insertRows(t, h, rows)
	now := h0.Add(4*time.Hour + 10*time.Minute)
	m, _ := newMaint(t, testCfg, now)

	runRollup(t, m, now)
	var before [3]hourlyRow
	for i := range before {
		before[i] = readHourly(t, h, h0.Add(time.Duration(i)*time.Hour))
		if !before[i].found {
			t.Fatalf("falta el resumen de la hora %d", i)
		}
	}
	var updatedAt time.Time
	if err := testPool.QueryRow(context.Background(), `SELECT updated_at FROM maintenance_state WHERE name = $1`, rollupMarker).Scan(&updatedAt); err != nil {
		t.Fatal(err)
	}

	runRollup(t, m, now) // sin datos nuevos: no debe cambiar ninguna fila (ni la marca)
	for i := range before {
		after := readHourly(t, h, h0.Add(time.Duration(i)*time.Hour))
		if after.xmin != before[i].xmin {
			t.Errorf("hora %d: la segunda corrida reescribio la fila (xmin %s -> %s)", i, before[i].xmin, after.xmin)
		}
	}
	var updatedAt2 time.Time
	if err := testPool.QueryRow(context.Background(), `SELECT updated_at FROM maintenance_state WHERE name = $1`, rollupMarker).Scan(&updatedAt2); err != nil {
		t.Fatal(err)
	}
	if !updatedAt2.Equal(updatedAt) {
		t.Errorf("la marca de agua se reescribio sin avanzar")
	}
}

// Las muestras tardias (buffer offline del agente) de las ultimas 3 horas ya resumidas se reprocesan;
// las de mas atras no.
func TestRollupReprocessesLateSamplesWithinThreeHours(t *testing.T) {
	fresh(t)
	h := mkHost(t, "srv-tardias")
	insertRows(t, h, minuteSeries(h0, 240, nil, func(int) float64 { return 10 }, nil, nil, nil)) // horas 10..13
	now := h0.Add(5*time.Hour + 30*time.Minute)                                                  // target 15:00 -> reprocesa 12:00..15:00
	m, _ := newMaint(t, testCfg, now)
	runRollup(t, m, now)

	if got := readHourly(t, h, h0.Add(3*time.Hour)); got.samples != 60 || !near(got.cpuAvg, 10) {
		t.Fatalf("hora 13 inicial got samples=%d cpu=%s", got.samples, show(got.cpuAvg))
	}
	// Tardia dentro de la ventana (13:59:30, cpu 100) y tardia fuera de ella (10:59:30).
	insertRows(t, h, []sampleRow{
		{ts: h0.Add(3*time.Hour + 59*time.Minute + 30*time.Second), cpu: f64(100)},
		{ts: h0.Add(59*time.Minute + 30*time.Second), cpu: f64(100)},
	})
	runRollup(t, m, now)

	if got := readHourly(t, h, h0.Add(3*time.Hour)); got.samples != 61 || !near(got.cpuMax, 100) {
		t.Errorf("hora 13 got samples=%d max=%s, want 61 y 100 (dato tardio reprocesado)", got.samples, show(got.cpuMax))
	}
	if got := readHourly(t, h, h0); got.samples != 60 {
		t.Errorf("hora 10 got samples=%d, want 60 (fuera de las 3 h reprocesadas: no se toca)", got.samples)
	}
}

// Tras un apagado largo la marca de agua avanza como mucho 48 h por ciclo hasta alcanzar la hora actual.
func TestRollupCatchesUpIn48hSteps(t *testing.T) {
	fresh(t)
	h := mkHost(t, "srv-recuperacion")
	start := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	var rows []sampleRow
	for d := range 6 { // 6 dias, dos muestras por dia (00:00 y 12:00)
		for _, hh := range []int{0, 12} {
			rows = append(rows, sampleRow{ts: start.AddDate(0, 0, d).Add(time.Duration(hh) * time.Hour), cpu: f64(float64(d*100 + hh))})
		}
	}
	insertRows(t, h, rows)
	now := time.Date(2026, 9, 1, 10, 30, 0, 0, time.UTC)
	m, _ := newMaint(t, testCfg, now)

	runRollup(t, m, now)
	if got, want := throughOf(t), start.Add(48*time.Hour); !got.Equal(want) {
		t.Fatalf("marca tras el ciclo 1 got %v, want %v", got, want)
	}
	if got := count(t, `SELECT count(*) FROM metrics_hourly`); got != 4 {
		t.Errorf("resumenes tras el ciclo 1 got %d, want 4 (dos dias)", got)
	}
	runRollup(t, m, now)
	if got, want := throughOf(t), start.Add(96*time.Hour); !got.Equal(want) {
		t.Fatalf("marca tras el ciclo 2 got %v, want %v", got, want)
	}
	// Los ciclos siguientes llegan a la hora actual y se quedan ahi.
	for range 10 {
		runRollup(t, m, now)
	}
	if got, want := throughOf(t), now.Truncate(time.Hour); !got.Equal(want) {
		t.Errorf("marca final got %v, want %v", got, want)
	}
	if got := count(t, `SELECT count(*) FROM metrics_hourly`); got != 12 {
		t.Errorf("resumenes finales got %d, want 12 (sin duplicados ni perdidas)", got)
	}
}

func TestRollupWithoutDataOnlySetsWatermark(t *testing.T) {
	fresh(t)
	now := h0.Add(30 * time.Minute)
	m, _ := newMaint(t, testCfg, now)
	runRollup(t, m, now)
	if got, want := throughOf(t), h0; !got.Equal(want) {
		t.Errorf("marca got %v, want %v", got, want)
	}
	if got := count(t, `SELECT count(*) FROM metrics_hourly`); got != 0 {
		t.Errorf("resumenes got %d, want 0", got)
	}
}

// La marca de agua solo avanza: correr el resumen con un reloj atrasado no la baja.
func TestRollupNeverMovesWatermarkBack(t *testing.T) {
	fresh(t)
	future := h0.Add(48 * time.Hour)
	setThrough(t, future)
	m, _ := newMaint(t, testCfg, h0)
	runRollup(t, m, h0.Add(6*time.Hour))
	if got := throughOf(t); !got.Equal(future) {
		t.Errorf("marca got %v, want %v (no debe retroceder)", got, future)
	}
}

// Los rangos largos (resumenes por hora) coinciden con la serie cruda equivalente.
func TestHourlySeriesMatchesRawSeries(t *testing.T) {
	fresh(t)
	ctx := context.Background()
	h := mkHost(t, "srv-comparacion")
	start := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)
	const minutes = 72 * 60

	// La tasa de red cambia cada hora (100..500 B/s); cada muestra usa la tasa de su propia hora.
	rate := func(i int) int64 { return 100 * int64(1+(i/60)%5) }
	rx := make([]int64, minutes)
	for i := 1; i < minutes; i++ {
		rx[i] = rx[i-1] + rate(i)*60
	}
	rows := minuteSeries(start, minutes, nil,
		func(i int) float64 { return 20 + 10*math.Sin(float64(i)/40) },
		func(i int) int { return 1000 + i%97 },
		func(i int) int64 { return rx[i] }, nil)
	insertRows(t, h, rows)

	now := start.AddDate(0, 0, 3).Add(6*time.Hour + 30*time.Minute)
	m, _ := newMaint(t, testCfg, now)
	for range 3 { // 72 h de datos + horas vacias: alcanza con 48 h por ciclo
		runRollup(t, m, now)
	}

	s := store.New(testPool)
	const step = 3 * time.Hour
	raw, err := s.MetricSeries(ctx, h, start, step)
	if err != nil {
		t.Fatal(err)
	}
	hourly, err := s.MetricSeriesHourly(ctx, h, start, step)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 24 || len(hourly) != 24 {
		t.Fatalf("buckets crudo/horario got %d/%d, want 24/24", len(raw), len(hourly))
	}
	for i := range raw {
		if !raw[i].TS.Equal(hourly[i].TS) {
			t.Fatalf("bucket %d: ts crudo %v != horario %v", i, raw[i].TS, hourly[i].TS)
		}
		// cpu y memoria son promedios exactos de las muestras (peso = samples); cpu_avg se guarda en REAL.
		if a, b := *raw[i].CPUPct, *hourly[i].CPUPct; math.Abs(a-b) > 1e-3 {
			t.Errorf("bucket %d cpu crudo %.5f != horario %.5f", i, a, b)
		}
		if a, b := *raw[i].MemUsedMB, *hourly[i].MemUsedMB; math.Abs(a-b) > 1 { // el resumen redondea mem a MB entero por hora
			t.Errorf("bucket %d mem crudo %.3f != horario %.3f", i, a, b)
		}
		if raw[i].NetRxBps == nil || hourly[i].NetRxBps == nil {
			t.Fatalf("bucket %d: tasa rx nula (crudo=%v horario=%v)", i, raw[i].NetRxBps, hourly[i].NetRxBps)
		}
		if a, b := *raw[i].NetRxBps, *hourly[i].NetRxBps; math.Abs(a-b)/a > 0.02 {
			t.Errorf("bucket %d rx crudo %.2f != horario %.2f (>2%%)", i, a, b)
		}
		if hourly[i].NetTxBps != nil {
			t.Errorf("bucket %d: tx horario got %v, want nil (sin contador)", i, *hourly[i].NetTxBps)
		}
	}
}
