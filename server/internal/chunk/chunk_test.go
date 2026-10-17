package chunk

import (
	"bytes"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"math"
	"math/rand/v2"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"
)

func fp(v float32) *float32 { return &v }
func ip32(v int32) *int32   { return &v }
func ip64(v int64) *int64   { return &v }
func ip16(v int16) *int16   { return &v }
func bp(v bool) *bool       { return &v }

var t0 = time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)

// want devuelve la fila tal como debe volver de Decode: floats redondeados a 2 decimales.
func want(r Row) Row {
	for _, p := range []**float32{&r.CPUPct, &r.Load1, &r.Load5, &r.Load15} {
		if *p != nil {
			*p = fp(Round2(**p))
		}
	}
	r.TS = r.TS.UTC()
	return r
}

func roundTrip(t *testing.T, rows []Row) {
	t.Helper()
	blob, err := Encode(rows)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := Decode(blob)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(got) != len(rows) {
		t.Fatalf("len = %d, want %d", len(got), len(rows))
	}
	for i := range rows {
		if w := want(rows[i]); !reflect.DeepEqual(got[i], w) {
			t.Fatalf("fila %d:\n got  %+v\n want %+v", i, got[i], w)
		}
	}
}

func TestRoundTripEdgeCases(t *testing.T) {
	full := Row{TS: t0, CPUPct: fp(12.345), MemUsedMB: ip32(1000), MemTotalMB: ip32(16000), NetRxBytes: ip64(5), NetTxBytes: ip64(7),
		SwapUsedMB: ip32(1), SwapTotalMB: ip32(2), Load1: fp(0.55), Load5: fp(0.5), Load15: fp(0.1), BatteryPct: ip16(80), BatteryCharging: bp(true)}
	cases := map[string][]Row{
		"vacio":    {},
		"una fila": {full},
		"solo ts":  {{TS: t0}, {TS: t0.Add(time.Second)}},
		"nulls alternos": {
			full, {TS: t0.Add(30 * time.Second)}, full, {TS: t0.Add(90 * time.Second), CPUPct: fp(1)}, {TS: t0.Add(91 * time.Second), BatteryCharging: bp(false)},
		},
		"reinicio de contador (baja)": {
			{TS: t0, NetRxBytes: ip64(1_000_000)}, {TS: t0.Add(time.Second), NetRxBytes: ip64(10)}, {TS: t0.Add(2 * time.Second), NetRxBytes: ip64(0)},
		},
		"huecos e irregular": {
			{TS: t0}, {TS: t0.Add(30 * time.Second)}, {TS: t0.Add(31 * time.Second)}, {TS: t0.Add(5 * time.Hour)}, {TS: t0.Add(5*time.Hour + time.Microsecond)},
		},
		"negativos y extremos": {
			{TS: t0, NetRxBytes: ip64(math.MaxInt64), NetTxBytes: ip64(math.MinInt64), MemUsedMB: ip32(math.MinInt32), MemTotalMB: ip32(math.MaxInt32),
				CPUPct: fp(-5.5), BatteryPct: ip16(math.MinInt16)},
			{TS: t0.Add(time.Second), NetRxBytes: ip64(math.MinInt64), NetTxBytes: ip64(math.MaxInt64), MemUsedMB: ip32(math.MaxInt32),
				MemTotalMB: ip32(math.MinInt32), CPUPct: fp(100), BatteryPct: ip16(math.MaxInt16)},
		},
		"ts desordenado": {{TS: t0.Add(time.Hour)}, {TS: t0}},
		"ts extremos":    {{TS: time.UnixMicro(math.MinInt64 / 2)}, {TS: time.UnixMicro(math.MaxInt64 / 2)}},
	}
	for name, rows := range cases {
		t.Run(name, func(t *testing.T) { roundTrip(t, rows) })
	}
}

func TestNonFiniteFloatsBecomeNull(t *testing.T) {
	blob, err := Encode([]Row{{TS: t0, CPUPct: fp(float32(math.NaN())), Load1: fp(float32(math.Inf(1))), Load5: fp(1.5)}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(blob)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].CPUPct != nil || got[0].Load1 != nil || got[0].Load5 == nil || *got[0].Load5 != 1.5 {
		t.Errorf("got %+v, want cpu y load1 nil y load5 1.5", got[0])
	}
}

func TestHugeFloatsAreClamped(t *testing.T) {
	blob, err := Encode([]Row{{TS: t0, CPUPct: fp(math.MaxFloat32), Load1: fp(-math.MaxFloat32)}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(blob)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].CPUPct == nil || *got[0].CPUPct != float32(maxQuant/100) || *got[0].Load1 != -float32(maxQuant/100) {
		t.Errorf("got %+v, want valores recortados a +-%g", got[0], maxQuant/100)
	}
}

func TestRoundingIsTwoDecimals(t *testing.T) {
	for _, tc := range []struct{ in, want float32 }{{12.345678, 12.35}, {0.004, 0}, {0.006, 0.01}, {99.999, 100}, {-1.234, -1.23}} {
		if got := Round2(tc.in); got != tc.want {
			t.Errorf("Round2(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// randomRows arma filas con NULLs, huecos, contadores que suben y reinicios, todo con una semilla fija.
func randomRows(seed uint64, n int) []Row {
	rng := rand.New(rand.NewPCG(seed, seed+1))
	rows := make([]Row, 0, n)
	ts := t0
	rx := rng.Int64N(1 << 40)
	for range n {
		ts = ts.Add(time.Duration(rng.IntN(40_000_000)+1) * time.Microsecond)
		r := Row{TS: ts}
		if rng.IntN(5) > 0 {
			r.CPUPct = fp(float32(rng.Float64() * 100))
		}
		if rng.IntN(4) > 0 {
			r.MemUsedMB = ip32(int32(rng.IntN(1 << 20)))
			r.MemTotalMB = ip32(int32(rng.IntN(1 << 20)))
		}
		if rng.IntN(4) > 0 {
			if rng.IntN(50) == 0 {
				rx = rng.Int64N(1000)
			} else {
				rx += rng.Int64N(1 << 20)
			}
			r.NetRxBytes, r.NetTxBytes = ip64(rx), ip64(rng.Int64())
		}
		if rng.IntN(3) == 0 {
			r.Load1, r.Load5, r.Load15 = fp(float32(rng.Float64()*20)), fp(float32(rng.Float64()*20)), nil
		}
		if rng.IntN(3) == 0 {
			r.SwapUsedMB, r.SwapTotalMB = ip32(int32(rng.IntN(5000))), ip32(int32(rng.IntN(5000)))
		}
		if rng.IntN(6) == 0 {
			r.BatteryPct, r.BatteryCharging = ip16(int16(rng.IntN(101))), bp(rng.IntN(2) == 0)
		}
		rows = append(rows, r)
	}
	return rows
}

func TestRoundTripProperty(t *testing.T) {
	for seed := range uint64(40) {
		roundTrip(t, randomRows(seed, int(seed)*37))
	}
}

func TestEncodeIsDeterministic(t *testing.T) {
	rows := randomRows(7, 500)
	a, err1 := Encode(rows)
	b, err2 := Encode(rows)
	if err1 != nil || err2 != nil || !bytes.Equal(a, b) {
		t.Errorf("dos Encode de las mismas filas difieren (%v, %v)", err1, err2)
	}
}

func TestEncodeRejectsTooManyRows(t *testing.T) {
	if _, err := Encode(make([]Row, MaxRows+1)); !errors.Is(err, ErrTooManyRows) {
		t.Errorf("err = %v, want ErrTooManyRows", err)
	}
}

// goldenRows son las filas de los vectores dorados.
func goldenRows() []Row {
	return []Row{
		{TS: time.UnixMicro(1_000_000), CPUPct: fp(1.5), MemUsedMB: ip32(100), NetRxBytes: ip64(1000)},
		{TS: time.UnixMicro(2_000_000), CPUPct: fp(2.25), MemUsedMB: ip32(101), NetRxBytes: ip64(1500)},
		{TS: time.UnixMicro(3_000_500), MemUsedMB: ip32(99), NetRxBytes: ip64(10), BatteryPct: ip16(50), BatteryCharging: bp(false)},
	}
}

// TestGoldenPayload fija el formato: si cambia el contenido sin comprimir, cambia la version del formato.
func TestGoldenPayload(t *testing.T) {
	rows := goldenRows()
	ts := make([]int64, len(rows))
	cols := make([]col, numCols)
	for i := range cols {
		cols[i] = newCol(len(rows))
	}
	for i, r := range rows {
		ts[i] = r.TS.UnixMicro()
		cols[cCPU].setF32(i, r.CPUPct)
		cols[cMemU].setI32(i, r.MemUsedMB)
		cols[cRx].setI64(i, r.NetRxBytes)
		if r.BatteryPct != nil {
			cols[cBatPct].set(i, int64(*r.BatteryPct))
			cols[cBatChg].set(i, 0)
		}
	}
	got := hex.EncodeToString(marshalPayload(ts, cols))
	if got != goldenHex {
		t.Errorf("payload = %s\nwant      %s", got, goldenHex)
	}
}

// goldenBlob es un blob real generado con Version 1: Decode debe seguir leyendolo (compatibilidad hacia atras).
func TestGoldenBlobDecodes(t *testing.T) {
	blob, err := hex.DecodeString(goldenBlobHex)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(blob)
	if err != nil {
		t.Fatalf("Decode del vector dorado: %v", err)
	}
	want := goldenRows()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if w := wantRow(want[i]); !reflect.DeepEqual(got[i], w) {
			t.Errorf("fila %d: got %+v want %+v", i, got[i], w)
		}
	}
}

func wantRow(r Row) Row { return want(r) }

func TestCorruptionIsDetected(t *testing.T) {
	blob, err := Encode(randomRows(3, 300))
	if err != nil {
		t.Fatal(err)
	}
	for _, pos := range []int{0, 1, 5, 8, 9, 10, len(blob) / 2, len(blob) - 1} {
		bad := bytes.Clone(blob)
		bad[pos] ^= 0x55
		if _, err := Decode(bad); err == nil {
			t.Errorf("Decode con el byte %d alterado no fallo", pos)
		}
	}
	for _, cut := range []int{0, 5, 9, len(blob) / 2, len(blob) - 1} {
		if _, err := Decode(blob[:cut]); err == nil {
			t.Errorf("Decode truncado a %d bytes no fallo", cut)
		}
	}
	bad := bytes.Clone(blob)
	bad[0] = 99
	if _, err := Decode(bad); !errors.Is(err, ErrVersion) {
		t.Errorf("version desconocida: err = %v, want ErrVersion", err)
	}
	bad = bytes.Clone(blob)
	bad[5] ^= 1
	if _, err := Decode(bad); !errors.Is(err, ErrCorrupt) {
		t.Errorf("crc alterado: err = %v, want ErrCorrupt", err)
	}
}

// realRows lee el fixture de metricas reales (epoch s, cpu, mem usada, mem total, rx, tx, swap usado, swap total, load1/5/15; -1 = NULL).
func realRows(t testing.TB) []Row {
	t.Helper()
	f, err := os.Open("testdata/real_metrics.csv")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Errorf("cerrando el fixture: %v", err)
		}
	})
	recs, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	num := func(s string) (float64, bool) {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			t.Fatalf("csv: %v", err)
		}
		return v, v != -1
	}
	rows := make([]Row, 0, len(recs))
	for _, rec := range recs {
		sec, _ := num(rec[0])
		r := Row{TS: time.Unix(int64(sec), 0).UTC()}
		if v, ok := num(rec[1]); ok {
			r.CPUPct = fp(float32(v))
		}
		if v, ok := num(rec[2]); ok {
			r.MemUsedMB = ip32(int32(v))
		}
		if v, ok := num(rec[3]); ok {
			r.MemTotalMB = ip32(int32(v))
		}
		if v, ok := num(rec[4]); ok {
			r.NetRxBytes = ip64(int64(v))
		}
		if v, ok := num(rec[5]); ok {
			r.NetTxBytes = ip64(int64(v))
		}
		if v, ok := num(rec[6]); ok {
			r.SwapUsedMB = ip32(int32(v))
		}
		if v, ok := num(rec[7]); ok {
			r.SwapTotalMB = ip32(int32(v))
		}
		if v, ok := num(rec[8]); ok {
			r.Load1 = fp(float32(v))
		}
		if v, ok := num(rec[9]); ok {
			r.Load5 = fp(float32(v))
		}
		if v, ok := num(rec[10]); ok {
			r.Load15 = fp(float32(v))
		}
		rows = append(rows, r)
	}
	return rows
}

// maxBytesPerSample es el techo del ratio sobre metricas reales (medido ~8 B; la fila de Postgres pesa 178 B).
const maxBytesPerSample = 12.0

func TestRatioOnRealMetrics(t *testing.T) {
	rows := realRows(t)
	blob, err := Encode(rows)
	if err != nil {
		t.Fatal(err)
	}
	ratio := float64(len(blob)) / float64(len(rows))
	t.Logf("%d muestras reales -> %d bytes = %.2f B/muestra (fila de Postgres: 178 B, %.1fx menos)", len(rows), len(blob), ratio, 178/ratio)
	if ratio > maxBytesPerSample {
		t.Errorf("%.2f B/muestra, want <= %.1f: el codec empeoro", ratio, maxBytesPerSample)
	}
	roundTrip(t, rows)
}

func FuzzDecode(f *testing.F) {
	good, err := Encode(randomRows(1, 20))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(good)
	f.Add([]byte{})
	f.Add([]byte{Version, 0xff, 0xff, 0xff, 0xff, 0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		rows, err := Decode(data)
		if err != nil {
			return
		}
		// Lo que se decodifica bien se puede volver a codificar y decodificar igual.
		blob, err := Encode(rows)
		if err != nil {
			t.Fatalf("Encode de filas decodificadas: %v", err)
		}
		again, err := Decode(blob)
		if err != nil || !reflect.DeepEqual(rows, again) {
			t.Fatalf("re-codificar cambio las filas: %v", err)
		}
	})
}

func FuzzRoundTrip(f *testing.F) {
	f.Add(uint64(1), 10)
	f.Add(uint64(99), 0)
	f.Fuzz(func(t *testing.T, seed uint64, n int) {
		if n < 0 || n > 2000 {
			return
		}
		roundTrip(t, randomRows(seed, n))
	})
}

// Vectores dorados generados con Version 1.
const (
	goldenHex     = "80897a00e807020201ac0296010103c80102030200030103d00fe807a31702000302000302000302000302000302000303000201640300020100"
	goldenBlobHex = "01030000001b8f3e5b6ae8ac6278c1cec4c4b886691a2323f30946266626066646e60bfc2fd8178b333130a321660626c61410c1001800"
)
