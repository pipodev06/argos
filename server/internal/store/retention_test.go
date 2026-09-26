package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"argos/server/internal/chunk"
)

func retF32(v float32) *float32 { return &v }

// retRowsAt arma n muestras cada 30 s desde start con cpu = cpuBase + i.
func retRowsAt(start time.Time, n int, cpuBase float32) []chunk.Row {
	rows := make([]chunk.Row, n)
	for i := range rows {
		mem, tot := int32(1000+i), int32(2000)
		rows[i] = chunk.Row{TS: start.Add(time.Duration(i) * 30 * time.Second), CPUPct: retF32(cpuBase + float32(i)), MemUsedMB: &mem, MemTotalMB: &tot}
	}
	return rows
}

func retInsertRaw(t *testing.T, hostID string, rows []chunk.Row) {
	t.Helper()
	for _, r := range rows {
		mustExec(t, `INSERT INTO metrics (host_id, ts, cpu_pct, mem_used_mb, mem_total_mb) VALUES ($1, $2, $3, $4, $5)`, hostID, r.TS, r.CPUPct, r.MemUsedMB, r.MemTotalMB)
	}
}

func retInsertChunk(t *testing.T, hostID string, day time.Time, rows []chunk.Row) {
	t.Helper()
	blob, err := chunk.Encode(rows)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, `INSERT INTO metrics_chunks (host_id, day, n, codec, data) VALUES ($1, $2::date, $3, $4, $5)`, hostID, day, len(rows), int16(chunk.Version), blob)
}

func retCollect(t *testing.T, s *Store, hostID string, from, to time.Time) []chunk.Row {
	t.Helper()
	var got []chunk.Row
	if err := s.DetailRange(context.Background(), hostID, from, to, func(r DetailSample) error {
		got = append(got, r)
		return nil
	}); err != nil {
		t.Fatalf("DetailRange: %v", err)
	}
	return got
}

// DetailRange mezcla dias con chunk (chunk manda aunque haya crudo) y dias crudos, en orden y respetando [from, to).
func TestDetailRange(t *testing.T) {
	s := newTestStore(t)
	h, other := mkHost(t, "srv-detalle"), mkHost(t, "srv-otro")
	day := func(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC) }

	c18, r19, c20, r21 := retRowsAt(day(18).Add(time.Hour), 4, 10), retRowsAt(day(19).Add(23*time.Hour+59*time.Minute), 4, 20), retRowsAt(day(20).Add(time.Hour), 4, 30), retRowsAt(day(21).Add(time.Hour), 4, 40)
	retInsertChunk(t, h, day(18), c18)
	retInsertRaw(t, h, retRowsAt(day(18).Add(2*time.Hour), 3, 99)) // crudo del dia 18: se ignora (el chunk manda)
	retInsertRaw(t, h, r19)                                        // cruza la medianoche hacia el dia 20, que tiene chunk: ese crudo se ignora
	retInsertChunk(t, h, day(20), c20)
	retInsertRaw(t, h, r21)
	retInsertRaw(t, other, retRowsAt(day(19).Add(time.Hour), 3, 77)) // otro host: nunca aparece
	retInsertChunk(t, other, day(20), retRowsAt(day(20).Add(time.Hour), 3, 88))

	round := func(rows []chunk.Row) []chunk.Row { // filas de un chunk vuelven con floats a 2 decimales
		out := make([]chunk.Row, len(rows))
		for i, r := range rows {
			r.CPUPct = retF32(chunk.Round2(*r.CPUPct))
			out[i] = r
		}
		return out
	}
	// r19 tiene 2 filas el dia 19 (23:59:00, 23:59:30) y 2 el dia 20 (00:00:00, 00:00:30; ese dia sale del chunk).
	want := append(append(append(round(c18), r19[:2]...), round(c20)...), r21...)

	got := retCollect(t, s, h, day(18), day(22))
	if len(got) != len(want) {
		t.Fatalf("filas = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Fatalf("fila %d:\n got  %+v\n want %+v", i, got[i], want[i])
		}
	}

	// Rango parcial: parte un chunk y un tramo crudo por tiempo, con [from, to).
	got = retCollect(t, s, h, c18[2].TS, r19[1].TS)
	if len(got) != 3 || !got[0].TS.Equal(c18[2].TS) || !got[2].TS.Equal(r19[0].TS) {
		t.Errorf("rango parcial = %d filas %v..%v, want 3 desde %v hasta %v", len(got), got[0].TS, got[len(got)-1].TS, c18[2].TS, r19[0].TS)
	}
	if got := retCollect(t, s, h, day(22), day(30)); len(got) != 0 {
		t.Errorf("rango sin datos = %d filas, want 0", len(got))
	}
	if got := retCollect(t, s, h, day(21), day(18)); len(got) != 0 {
		t.Errorf("from >= to = %d filas, want 0", len(got))
	}

	// Un error de fn detiene la lectura y se devuelve tal cual.
	boom := errors.New("boom")
	n := 0
	err := s.DetailRange(context.Background(), h, day(18), day(22), func(DetailSample) error {
		n++
		if n == 6 {
			return boom
		}
		return nil
	})
	if !errors.Is(err, boom) || n != 6 {
		t.Errorf("err = %v tras %d filas, want boom tras 6", err, n)
	}

	// Un chunk corrupto es un error (no se entregan datos falsos).
	mustExec(t, `UPDATE metrics_chunks SET data = overlay(data placing '\xff'::bytea from 20 for 1) WHERE host_id = $1 AND day = $2::date`, h, day(20))
	if err := s.DetailRange(context.Background(), h, day(20), day(21), func(DetailSample) error { return nil }); !errors.Is(err, chunk.ErrCorrupt) {
		t.Errorf("chunk corrupto: err = %v, want ErrCorrupt", err)
	}
}

func TestRetentionSettingsSourcesAndEffective(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	def := DefaultRetention()

	got, err := s.RetentionSettings(ctx)
	if err != nil || got != (RetentionSettings{DetailDays: 30, Source: SourceDefault, HotDays: 8, HourlyDays: 400}) {
		t.Fatalf("por defecto: %+v, %v", got, err)
	}
	env := s.WithRetention(RetentionDefaults{DetailDays: 45, DetailSource: SourceEnv, HotDays: 10, HourlyDays: 500})
	if got, err = env.RetentionSettings(ctx); err != nil || got != (RetentionSettings{DetailDays: 45, Source: SourceEnv, HotDays: 10, HourlyDays: 500}) {
		t.Fatalf("de entorno: %+v, %v", got, err)
	}
	if s.retention != def {
		t.Fatal("WithRetention no debe modificar el Store original")
	}
	user := mkUser(t, s, "admin-ret", RoleAdmin)
	if err := env.SetGlobalDetailDays(ctx, 90, user); err != nil {
		t.Fatal(err)
	}
	if got, err = env.RetentionSettings(ctx); err != nil || got.DetailDays != 90 || got.Source != SourceDB {
		t.Fatalf("de la BD: %+v, %v", got, err)
	}
	if err := env.SetGlobalDetailDays(ctx, 120, ""); err != nil { // sin autor
		t.Fatal(err)
	}
	if got, _ = env.RetentionSettings(ctx); got.DetailDays != 120 {
		t.Errorf("segundo valor = %d, want 120", got.DetailDays)
	}
	// Un valor ilegible en settings (no lo escribe la API) es un error, no un default silencioso.
	mustExec(t, `UPDATE settings SET value = '"x"'`)
	if _, err := s.RetentionSettings(ctx); err == nil {
		t.Error("un valor ilegible en settings debe ser un error")
	}
	mustExec(t, `DELETE FROM settings`)

	// EffectiveDetail: host > grupo > global.
	for _, tc := range []struct {
		host, group *int
		wantDays    int
		wantSrc     string
	}{
		{nil, nil, 30, SourceGlobal},
		{nil, retInt(20), 20, SourceGroup},
		{retInt(90), retInt(20), 90, SourceHost},
		{retInt(90), nil, 90, SourceHost},
	} {
		if d, src := EffectiveDetail(tc.host, tc.group, 30); d != tc.wantDays || src != tc.wantSrc {
			t.Errorf("EffectiveDetail(%v, %v) = %d, %q; want %d, %q", tc.host, tc.group, d, src, tc.wantDays, tc.wantSrc)
		}
	}

	// Con la BD: host inexistente = ErrNotFound; el CHECK rechaza valores fuera de rango.
	if _, _, err := s.EffectiveDetailDays(ctx, "00000000-0000-4000-8000-000000000000"); !errors.Is(err, ErrNotFound) {
		t.Errorf("host inexistente: err = %v, want ErrNotFound", err)
	}
	h := mkHost(t, "srv-efectivo")
	if err := s.SetHostDetailDays(ctx, h, retInt(7)); err == nil {
		t.Error("7 dias debe violar el CHECK 8..730")
	}
	if err := s.SetHostDetailDays(ctx, "00000000-0000-4000-8000-000000000000", retInt(20)); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetHostDetailDays de un host inexistente: err = %v, want ErrNotFound", err)
	}
	if err := s.SetHostDetailDays(ctx, h, retInt(400)); err != nil {
		t.Fatal(err)
	}
	if d, src, err := s.EffectiveDetailDays(ctx, h); err != nil || d != 400 || src != SourceHost {
		t.Errorf("EffectiveDetailDays = %d, %q, %v; want 400, host", d, src, err)
	}
}

func retInt(v int) *int { return &v }

func TestPreviewRetention(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	today := time.Date(2026, 9, 26, 15, 0, 0, 0, time.UTC)
	dayOf := func(k int) time.Time { return time.Date(2026, 9, 26-k, 0, 0, 0, 0, time.UTC) }

	h := mkHost(t, "srv-prev")
	var g string
	if err := testPool.QueryRow(ctx, `INSERT INTO host_groups (name) VALUES ('grp-prev') RETURNING id`).Scan(&g); err != nil {
		t.Fatal(err)
	}
	mustExec(t, `UPDATE hosts SET group_id = $2 WHERE id = $1`, h, g)
	for _, k := range []int{5, 25, 29, 30, 31, 100} {
		retInsertChunk(t, h, dayOf(k), nil)
	}

	// Global 30 -> 20: se pierden los chunks con 20 < k <= 30 que existen (25, 29, 30): 3. El de k=31 ya estaba fuera.
	p, err := s.PreviewRetention(ctx, RetentionTarget{Kind: TargetGlobal}, retInt(20), today)
	if err != nil || p != (ShrinkPreview{DaysLost: 10, Chunks: 3, EstBytes: 3 * EstChunkBytes}) {
		t.Errorf("global 30->20 = %+v, %v", p, err)
	}
	// Subir o dejar igual no pierde nada.
	for _, d := range []int{30, 31, 400} {
		if p, err := s.PreviewRetention(ctx, RetentionTarget{Kind: TargetGlobal}, retInt(d), today); err != nil || p != (ShrinkPreview{}) {
			t.Errorf("global 30->%d = %+v, %v; want vacio", d, p, err)
		}
	}
	// Un host con valor propio o cuyo grupo tiene valor no cambia con el global.
	mustExec(t, `UPDATE host_groups SET detail_retention_days = 60 WHERE id = $1`, g)
	if p, err := s.PreviewRetention(ctx, RetentionTarget{Kind: TargetGlobal}, retInt(10), today); err != nil || p.Chunks != 0 {
		t.Errorf("global con el grupo sobrescrito = %+v, %v; want 0 chunks", p, err)
	}
	// Grupo 60 -> heredar (30): los chunks con 30 < k <= 60 que existen: solo k=31.
	if p, err := s.PreviewRetention(ctx, RetentionTarget{Kind: TargetGroup, ID: g}, nil, today); err != nil || p != (ShrinkPreview{DaysLost: 30, Chunks: 1, EstBytes: EstChunkBytes}) {
		t.Errorf("grupo 60->heredar = %+v, %v", p, err)
	}
	// Host: hereda 60 del grupo -> 8 propio: k en (8, 60]: 25, 29, 30, 31 = 4.
	if p, err := s.PreviewRetention(ctx, RetentionTarget{Kind: TargetHost, ID: h}, retInt(8), today); err != nil || p != (ShrinkPreview{DaysLost: 52, Chunks: 4, EstBytes: 4 * EstChunkBytes}) {
		t.Errorf("host 60->8 = %+v, %v", p, err)
	}

	for _, tc := range []struct {
		name string
		t    RetentionTarget
		days *int
		want error
	}{
		{"grupo inexistente", RetentionTarget{Kind: TargetGroup, ID: "00000000-0000-4000-8000-000000000000"}, retInt(9), ErrNotFound},
		{"host inexistente", RetentionTarget{Kind: TargetHost, ID: "00000000-0000-4000-8000-000000000000"}, retInt(9), ErrNotFound},
	} {
		if _, err := s.PreviewRetention(ctx, tc.t, tc.days, today); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	if _, err := s.PreviewRetention(ctx, RetentionTarget{Kind: TargetGlobal}, nil, today); err == nil {
		t.Error("el global no puede heredar: debe ser un error")
	}
	if _, err := s.PreviewRetention(ctx, RetentionTarget{Kind: "otro"}, retInt(9), today); err == nil {
		t.Error("un blanco desconocido debe ser un error")
	}
}
