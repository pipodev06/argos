package retention

import (
	"context"
	"strings"
	"testing"
	"time"

	"argos/server/internal/chunk"
	"argos/server/internal/store"
)

// insertChunkDays crea un chunk vacio por cada dia (hoy - k) de ks.
func insertChunkDays(t *testing.T, hostID string, today time.Time, ks []int) {
	t.Helper()
	blob, err := chunk.Encode(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range ks {
		mustExec(t, `INSERT INTO metrics_chunks (host_id, day, n, codec, data) VALUES ($1, $2::date, 0, $3, $4)`, hostID, today.AddDate(0, 0, -k), int16(chunk.Version), blob)
	}
}

// keptDays devuelve cuantos chunks del host siguen y el mas viejo (en dias atras).
func keptDays(t *testing.T, hostID string, today time.Time) (n, oldestK int) {
	t.Helper()
	var oldest *time.Time
	if err := testPool.QueryRow(context.Background(), `SELECT count(*)::int, min(day) FROM metrics_chunks WHERE host_id = $1`, hostID).Scan(&n, &oldest); err != nil {
		t.Fatal(err)
	}
	if oldest != nil {
		oldestK = int(today.Sub(oldest.UTC()).Hours() / 24)
	}
	return n, oldestK
}

// Retencion efectiva host > grupo > global: el recorte solo toca a los hosts vencidos segun la suya, y los cambios
// (global desde settings) rigen al siguiente ciclo sin reiniciar. El SQL del mantenimiento coincide con store.EffectiveDetailDays.
func TestTrimUsesEffectiveRetention(t *testing.T) {
	fresh(t)
	ctx := context.Background()
	now := farNow()
	today := utcDay(now)
	m, logs := newMaint(t, testCfg, now) // detalle global de arranque: 30 dias
	st := store.New(testPool)

	var g20, g20b string
	if err := testPool.QueryRow(ctx, `INSERT INTO host_groups (name, detail_retention_days) VALUES ('g20', 20) RETURNING id`).Scan(&g20); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `INSERT INTO host_groups (name, detail_retention_days) VALUES ('g20b', 20) RETURNING id`).Scan(&g20b); err != nil {
		t.Fatal(err)
	}
	var gNone string
	if err := testPool.QueryRow(ctx, `INSERT INTO host_groups (name) VALUES ('sin-valor') RETURNING id`).Scan(&gNone); err != nil {
		t.Fatal(err)
	}
	hosts := map[string]struct {
		id        string
		wantDays  int
		wantSrc   string
		afterDays int // tras subir/bajar el global a 15
	}{}
	mk := func(name string, group *string, own *int, wantDays int, wantSrc string, after int) {
		id := mkHost(t, name)
		mustExec(t, `UPDATE hosts SET group_id = $2, detail_retention_days = $3 WHERE id = $1`, id, group, own)
		hosts[name] = struct {
			id        string
			wantDays  int
			wantSrc   string
			afterDays int
		}{id, wantDays, wantSrc, after}
	}
	d60, d90 := 60, 90
	mk("global", nil, nil, 30, store.SourceGlobal, 15)
	mk("propio", nil, &d60, 60, store.SourceHost, 60)
	mk("grupo", &g20, nil, 20, store.SourceGroup, 20)
	mk("host-sobre-grupo", &g20b, &d90, 90, store.SourceHost, 90)
	mk("grupo-sin-valor", &gNone, nil, 30, store.SourceGlobal, 15)

	ks := []int{10, 14, 15, 16, 19, 20, 21, 29, 30, 31, 59, 60, 61, 89, 90, 91}
	for _, h := range hosts {
		insertChunkDays(t, h.id, today, ks)
	}

	for name, h := range hosts {
		days, src, err := st.EffectiveDetailDays(ctx, h.id)
		if err != nil || days != h.wantDays || src != h.wantSrc {
			t.Errorf("%s: EffectiveDetailDays = %d, %q, %v; want %d, %q", name, days, src, err, h.wantDays, h.wantSrc)
		}
	}

	m.RunOnce(ctx)
	logs.noErrors(t)
	expect := func(phase string, effective func(name string) int) {
		t.Helper()
		for name, h := range hosts {
			want, wantOldest := 0, 0
			for _, k := range ks {
				if k <= effective(name) {
					want++
					wantOldest = max(wantOldest, k)
				}
			}
			if n, oldest := keptDays(t, h.id, today); n != want || oldest != wantOldest {
				t.Errorf("%s, %s: quedan %d chunks (el mas viejo a %d d), want %d (a %d d)", phase, name, n, oldest, want, wantOldest)
			}
		}
	}
	expect("global 30", func(name string) int { return hosts[name].wantDays })

	// Bajar el global (desde la API, sin reiniciar) recorta solo a los hosts que lo heredan.
	if err := st.SetGlobalDetailDays(ctx, 15, ""); err != nil {
		t.Fatal(err)
	}
	m.RunOnce(ctx)
	logs.noErrors(t)
	expect("global 15", func(name string) int { return hosts[name].afterDays })

	// Subir vale hacia adelante: lo ya borrado no vuelve.
	if err := st.SetGlobalDetailDays(ctx, 100, ""); err != nil {
		t.Fatal(err)
	}
	m.RunOnce(ctx)
	expect("global 100", func(name string) int { return hosts[name].afterDays })
}

// Un valor ilegible en settings no borra de mas: se usa la retencion de arranque y se avisa.
func TestTrimFallsBackOnInvalidSetting(t *testing.T) {
	fresh(t)
	ctx := context.Background()
	now := farNow()
	today := utcDay(now)
	m, logs := newMaint(t, testCfg, now)
	h := mkHost(t, "srv-ajuste-roto")
	insertChunkDays(t, h, today, []int{29, 31})
	mustExec(t, `INSERT INTO settings (key, value) VALUES ('metrics_detail_days', '"treinta"')`)

	m.RunOnce(ctx)
	if n, _ := keptDays(t, h, today); n != 1 {
		t.Errorf("chunks tras el recorte con el ajuste roto = %d, want 1 (30 dias de arranque)", n)
	}
	if got := logs.String(); !strings.Contains(got, "valor invalido de settings") {
		t.Errorf("debia avisar del valor invalido:\n%s", got)
	}
}
