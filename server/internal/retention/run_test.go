package retention

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// holdLock toma el lock advisory del mantenimiento desde otra sesion, como si otra instancia estuviera trabajando.
func holdLock(t *testing.T) (release func()) {
	t.Helper()
	ctx := context.Background()
	pc, err := testPool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got bool
	if err := pc.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext($1))`, lockName).Scan(&got); err != nil || !got {
		pc.Release()
		t.Fatalf("no se pudo tomar el lock de prueba (got=%v, err=%v)", got, err)
	}
	return func() {
		if _, err := pc.Exec(ctx, `SELECT pg_advisory_unlock(hashtext($1))`, lockName); err != nil {
			t.Errorf("liberando el lock de prueba: %v", err)
		}
		pc.Release()
	}
}

func TestRunOnceSkipsWhenAnotherInstanceHoldsTheLock(t *testing.T) {
	fresh(t)
	ctx := context.Background()
	now := farNow()
	m, logs := newMaint(t, testCfg, now)
	before := len(partNames(t, tableMetrics))

	release := holdLock(t)
	if m.RunOnce(ctx) {
		t.Errorf("RunOnce got ran=true con el lock tomado por otra sesion, want false")
	}
	if got := len(partNames(t, tableMetrics)); got != before {
		t.Errorf("particiones tras el ciclo saltado got %d, want %d (no debe hacer nada)", got, before)
	}
	if got := count(t, `SELECT count(*) FROM maintenance_state`); got != 0 {
		t.Errorf("el ciclo saltado escribio la marca de agua")
	}
	release()

	if !m.RunOnce(ctx) {
		t.Fatalf("RunOnce got ran=false con el lock libre, want true")
	}
	if !hasPartition(t, tableMetrics, utcDay(now).AddDate(0, 0, createAheadDays)) {
		t.Errorf("el ciclo con el lock libre no creo las particiones")
	}
	logs.noErrors(t)
}

// Tras un ciclo la conexion vuelve al pool sin el lock advisory ni el lock_timeout del mantenimiento.
func TestRunOnceLeavesConnectionClean(t *testing.T) {
	fresh(t)
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(testDSN)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns, cfg.MinConns = 1, 0 // una sola conexion: la siguiente Acquire es la misma sesion
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	m := New(pool, discardLogger(), testCfg, farNow)
	m.lockTimeout = 7 * time.Second
	if !m.RunOnce(ctx) {
		t.Fatal("RunOnce got ran=false")
	}

	var lockTimeout string
	if err := pool.QueryRow(ctx, `SHOW lock_timeout`).Scan(&lockTimeout); err != nil {
		t.Fatal(err)
	}
	if lockTimeout != "0" {
		t.Errorf("lock_timeout de la sesion devuelta al pool got %q, want 0", lockTimeout)
	}
	// Otra sesion puede tomar el lock: el ciclo lo libero.
	release := holdLock(t)
	release()
}

// Dos instancias a la vez: solo una trabaja por vez, ambas terminan sin errores y el resultado es el de una sola.
func TestRunOnceConcurrentInstances(t *testing.T) {
	fresh(t)
	ctx := context.Background()
	now := farNow()
	today := utcDay(now)
	h := mkHost(t, "srv-concurrente")
	// Datos en dias ya vencidos (crudo de 8 d) pa que el ciclo cree, resuma y borre.
	mkPartition(t, tableMetrics, today.AddDate(0, 0, -20))
	insertRows(t, h, minuteSeries(today.AddDate(0, 0, -20).Add(time.Hour), 120, nil,
		func(int) float64 { return 5 }, nil, nil, nil))

	const n = 4
	logs := make([]*logBuf, n)
	ran := make([]bool, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		var m *Maintainer
		m, logs[i] = newMaint(t, testCfg, now)
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ran[i] = m.RunOnce(ctx)
		}()
	}
	close(start)
	wg.Wait()

	anyRan := false
	for i := range n {
		anyRan = anyRan || ran[i]
		logs[i].noErrors(t)
	}
	if !anyRan {
		t.Errorf("ninguna instancia trabajo")
	}
	for _, table := range partitionedTables {
		for off := 0; off <= createAheadDays; off++ {
			if !hasPartition(t, table, today.AddDate(0, 0, off)) {
				t.Errorf("%s: falta la particion de hoy+%d", table, off)
			}
		}
	}
	// Un solo resumen por hora (sin duplicados: PK) y la particion vencida se borro una sola vez, ya resumida.
	if got := count(t, `SELECT count(*) FROM metrics_hourly WHERE host_id = $1`, h); got != 2 {
		t.Errorf("resumenes got %d, want 2", got)
	}
}

// El ciclo completo: crea, resume y borra en ese orden, y una particion vencida no se borra hasta que su resumen
// existe (la marca de agua avanza 48 h por ciclo).
func TestRunOnceDropsOnlyWhatIsAlreadySummarized(t *testing.T) {
	fresh(t)
	ctx := context.Background()
	now := farNow()
	today := utcDay(now)
	m, logs := newMaint(t, testCfg, now) // crudo 8 d: todo lo anterior a hoy-8 esta vencido
	m.packBudget = 100                   // aqui se prueba la guardia del resumen, no el limite de empaquetado por ciclo
	h := mkHost(t, "srv-ciclo")

	day := func(off int) time.Time { return today.AddDate(0, 0, off) }
	// Cinco dias vencidos consecutivos con una muestra al mediodia cada uno.
	for off := -20; off <= -16; off++ {
		mkPartition(t, tableMetrics, day(off))
		insertRows(t, h, []sampleRow{{ts: day(off).Add(12 * time.Hour), cpu: f64(float64(-off))}})
	}
	summarized := func() int { return count(t, `SELECT count(*) FROM metrics_hourly WHERE host_id = $1`, h) }

	// Ciclo 1: la marca arranca en la muestra mas vieja (dia -20 12:00) y avanza 48 h (hasta -18 12:00): se borran
	// los dias -20 y -19 (terminan <= -18 12:00) y sus resumenes (2) quedan; -18 termina en -17 y sigue.
	if !m.RunOnce(ctx) {
		t.Fatal("RunOnce got ran=false")
	}
	if got := summarized(); got != 2 {
		t.Errorf("ciclo 1: resumenes got %d, want 2", got)
	}
	for off, want := range map[int]bool{-20: false, -19: false, -18: true, -17: true, -16: true} {
		if got := hasPartition(t, tableMetrics, day(off)); got != want {
			t.Errorf("ciclo 1: particion del dia %+d existe=%v, want %v", off, got, want)
		}
	}
	if hasLegacy(t, tableMetrics) {
		t.Errorf("ciclo 1: metrics_legacy (vencida, sin datos, ya resumida) sigue existiendo")
	}

	// Ciclos 2 y 3: la marca llega a -16 12:00 y luego a -14 12:00: se van los dias -18, -17 y por ultimo -16.
	m.RunOnce(ctx)
	for off, want := range map[int]bool{-18: false, -17: false, -16: true} {
		if got := hasPartition(t, tableMetrics, day(off)); got != want {
			t.Errorf("ciclo 2: particion del dia %+d existe=%v, want %v", off, got, want)
		}
	}
	m.RunOnce(ctx)
	if hasPartition(t, tableMetrics, day(-16)) {
		t.Errorf("ciclo 3: la particion del dia -16 sigue existiendo pese a estar resumida y vencida")
	}
	if got := summarized(); got != 5 {
		t.Errorf("resumenes finales got %d, want 5 (uno por muestra, ninguno perdido al borrar el crudo)", got)
	}
	// Las particiones futuras siguen ahi.
	for off := 0; off <= createAheadDays; off++ {
		if !hasPartition(t, tableMetrics, day(off)) {
			t.Errorf("falta la particion de hoy+%d", off)
		}
	}
	logs.noErrors(t)
}

func TestRunStopsWithContextAndRunsCycles(t *testing.T) {
	fresh(t)
	now := farNow()
	m, _ := newMaint(t, testCfg, now)
	m.cfg.Interval = 20 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.Run(ctx)
	}()

	deadline := time.Now().Add(10 * time.Second)
	for !hasPartition(t, tableMetrics, utcDay(now).AddDate(0, 0, createAheadDays)) {
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("Run no creo las particiones en 10 s")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run no termino tras cancelar el contexto")
	}
}

// Un fallo en un paso se loguea y no impide los siguientes; y si el resumen falla, el crudo no se borra.
func TestRunOnceContinuesAfterStepFailure(t *testing.T) {
	fresh(t)
	ctx := context.Background()
	now := farNow()
	m, logs := newMaint(t, testCfg, now)
	h := mkHost(t, "srv-fallo")
	// Un dato viejo (cae en la particion legacy) pa que el resumen tenga algo que escribir... en una tabla que no existe.
	insertRows(t, h, minuteSeries(h0, 5, nil, func(int) float64 { return 1 }, nil, nil, nil))
	mustExec(t, `DROP TABLE metrics_hourly`)

	if !m.RunOnce(ctx) {
		t.Fatal("RunOnce got ran=false")
	}
	if !hasPartition(t, tableMetrics, utcDay(now).AddDate(0, 0, createAheadDays)) {
		t.Errorf("las particiones no se crearon (el paso 1 va antes del fallo)")
	}
	s := logs.String()
	for _, step := range []string{`"step":"rollup"`, `"step":"purge_hourly"`} {
		if !strings.Contains(s, step) {
			t.Errorf("no se logueo el fallo del paso %s:\n%s", step, s)
		}
	}
	// Sin resumen no hay marca de agua: el crudo vencido (legacy con el dato) no se borra.
	if !hasLegacy(t, tableMetrics) {
		t.Errorf("metrics_legacy se borro aunque el resumen fallo")
	}
	if got := count(t, `SELECT count(*) FROM metrics`); got != 5 {
		t.Errorf("filas de metrics got %d, want 5", got)
	}
}

func TestRunOnceReturnsFalseWhenContextIsCancelled(t *testing.T) {
	fresh(t)
	m, logs := newMaint(t, testCfg, farNow())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if m.RunOnce(ctx) {
		t.Errorf("RunOnce got ran=true con el contexto cancelado")
	}
	// Un apagado en curso no es un error: no se loguea.
	logs.noErrors(t)
}
