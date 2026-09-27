// Package retention mantiene acotadas las tablas de metricas: crea las particiones diarias
// (UTC) de `metrics`, `service_checks` y `disk_samples`, resume `metrics` por hora en `metrics_hourly` y
// borra lo vencido. Es el unico dueno de ese ciclo: `cmd/server` solo lo arranca.
//
// Un ciclo corre en una conexion dedicada del pool que toma un lock advisory de sesion
// (con varias instancias del server solo una trabaja; las demas saltan el ciclo) y fija
// lock_timeout, asi ninguna DDL espera indefinidamente a una consulta larga. Los pasos
// van en este orden y un fallo en uno se loguea sin frenar a los demas:
//
//  1. crear las particiones de hoy a hoy+7 dias;
//  2. resumir por hora las metricas crudas (marca de agua metrics_rollup_through);
//  3. borrar las particiones vencidas (las de `metrics` solo si ya estan resumidas y empaquetadas: antes de
//     borrarlas se guarda cada host-dia en `metrics_chunks`, ver pack.go; las de `disk_samples` se empaquetan
//     igual en `disk_chunks` antes de borrarse, con una ventana caliente fija sin marca de agua, ver pack_disk.go);
//  4. recortar `metrics_chunks` segun la retencion de detalle efectiva de cada host (trim.go) y `disk_chunks`
//     segun la retencion global unica de disco (pack_disk.go: disk_samples no tiene retencion por host/grupo);
//  5. purgar `metrics_hourly` vencido, en lotes.
package retention

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"argos/server/internal/beat"
)

const (
	// lockName da la clave del lock advisory (hashtext) que serializa el mantenimiento entre instancias.
	lockName = "argos.metrics_maintenance"

	// defaultLockTimeout acota la espera de cualquier lock de tabla (ATTACH, DETACH, DROP).
	defaultLockTimeout = 5 * time.Second

	// createAheadDays: se mantienen particiones hasta hoy+7 dias (UTC).
	createAheadDays = 7

	// unlockTimeout acota la liberacion del lock y del lock_timeout al terminar (el ctx del ciclo puede estar cancelado).
	unlockTimeout = 5 * time.Second

	// hourlyPurgeBatch es cuantas filas de metrics_hourly borra cada sentencia.
	hourlyPurgeBatch = 5000
)

// Config son los parametros del mantenimiento (ya validados por el paquete config).
type Config struct {
	// HotDays es la ventana caliente: cuanto se conserva `metrics` (particiones diarias) antes de empaquetarlo en
	// metrics_chunks (>= 8, el minimo que exige el resumen por hora y que cubre los 7 dias de retraso que admite el ingest).
	HotDays int
	// DetailDays es la retencion global de detalle (dias de chunks) cuando la tabla settings no fija otra; cada host
	// o grupo puede tener la suya (host ?? grupo ?? global).
	DetailDays int
	// HourlyDays es cuanto se conserva `metrics_hourly`.
	HourlyDays int
	// ServiceChecksDays es cuanto se conserva `service_checks`.
	ServiceChecksDays int
	// DiskDays es cuanto se conserva `disk_samples` (uso de disco por montaje, sin resumen por hora).
	DiskDays int
	// Interval es cada cuanto corre un ciclo.
	Interval time.Duration
}

// Maintainer ejecuta el ciclo de mantenimiento. Es seguro usarlo desde una sola goroutine (Run).
type Maintainer struct {
	pool        *pgxpool.Pool
	log         *slog.Logger
	cfg         Config
	now         func() time.Time
	lockTimeout time.Duration
	// packBudget es cuantas particiones con trabajo empaqueta un ciclo (los tests lo suben).
	packBudget int
	// beat anota el resultado de cada ciclo pa el auto-monitoreo (nil = no se anota).
	beat *beat.Beat
}

// WithBeat hace que cada ciclo anote su resultado en b (GET /system/status): OK si ningun paso fallo, Fail si
// alguno fallo o no se pudo preparar la conexion. Un ciclo saltado por el lock de otra instancia cuenta como OK.
func (m *Maintainer) WithBeat(b *beat.Beat) *Maintainer {
	m.beat = b
	return m
}

// New crea el mantenedor. now permite fijar el reloj en tests (time.Now en produccion).
func New(pool *pgxpool.Pool, log *slog.Logger, cfg Config, now func() time.Time) *Maintainer {
	return &Maintainer{pool: pool, log: log, cfg: cfg, now: now, lockTimeout: defaultLockTimeout, packBudget: defaultPackBudget}
}

// Run corre un ciclo al arrancar y luego uno cada cfg.Interval, hasta que ctx se cancele.
func (m *Maintainer) Run(ctx context.Context) {
	ticker := time.NewTicker(m.cfg.Interval)
	defer ticker.Stop()

	m.log.Info("mantenimiento de metricas activo",
		"hot_days", m.cfg.HotDays, "detail_days", m.cfg.DetailDays, "hourly_days", m.cfg.HourlyDays,
		"service_checks_days", m.cfg.ServiceChecksDays, "disk_days", m.cfg.DiskDays, "interval", m.cfg.Interval.String())
	for {
		m.RunOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RunOnce ejecuta un ciclo. Devuelve false si otra instancia tenia el lock (ciclo saltado) o si
// no se pudo preparar la conexion. Los errores de cada paso se loguean; no se devuelven.
func (m *Maintainer) RunOnce(ctx context.Context) (ran bool) {
	pc, err := m.pool.Acquire(ctx)
	if err != nil {
		m.logFailure(ctx, "adquiriendo conexion del mantenimiento", err)
		return false
	}
	defer m.release(pc)
	conn := pc.Conn()

	var locked bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext($1))`, lockName).Scan(&locked); err != nil {
		m.logFailure(ctx, "tomando el lock del mantenimiento", err)
		return false
	}
	if !locked {
		m.log.Debug("mantenimiento de metricas en curso en otra instancia: ciclo saltado")
		m.beat.OK(m.now())
		return false
	}
	// lock_timeout no admite parametros ($1); el valor sale de una constante o del test, nunca de input externo.
	if _, err := conn.Exec(ctx, fmt.Sprintf(`SET lock_timeout = %d`, m.lockTimeout.Milliseconds())); err != nil {
		m.logFailure(ctx, "fijando lock_timeout", err)
		return false
	}

	now := m.now()
	steps := []struct {
		name string
		run  func() error
	}{
		{"create_partitions", func() error { return m.createPartitions(ctx, conn, now) }},
		{"rollup", func() error { return m.rollup(ctx, conn, now) }},
		{"drop_partitions", func() error { return m.dropExpired(ctx, conn, now) }},
		{"trim_chunks", func() error { return m.trimChunks(ctx, conn, now) }},
		{"trim_disk_chunks", func() error { return m.trimDiskChunks(ctx, conn, now) }},
		{"purge_hourly", func() error { return m.purgeHourly(ctx, conn, now) }},
	}
	failed := false
	for _, s := range steps {
		if err := s.run(); err != nil {
			failed = true
			m.logFailure(ctx, "fallo un paso del mantenimiento de metricas", err, "step", s.name)
		}
	}
	if !failed {
		m.beat.OK(m.now())
	}
	return true
}

// release libera el lock advisory y restaura lock_timeout antes de devolver la conexion al pool.
// Si no se puede garantizar que la sesion quedo limpia, la conexion se cierra en vez de reutilizarse
// (un lock de sesion colgado bloquearia el mantenimiento de todas las instancias).
func (m *Maintainer) release(pc *pgxpool.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), unlockTimeout)
	defer cancel()

	conn := pc.Conn()
	var unlocked bool
	err := conn.QueryRow(ctx, `SELECT pg_advisory_unlock(hashtext($1))`, lockName).Scan(&unlocked)
	if err == nil {
		_, err = conn.Exec(ctx, `RESET lock_timeout`)
	}
	if err != nil {
		m.log.Warn("no se pudo limpiar la sesion del mantenimiento: se cierra la conexion", "err", err)
		if cerr := pc.Hijack().Close(ctx); cerr != nil {
			m.log.Warn("cerrando la conexion del mantenimiento", "err", cerr)
		}
		return
	}
	// unlocked=false = no teniamos el lock (ciclo saltado): normal.
	pc.Release()
}

// logFailure loguea un error, salvo que sea por el apagado en curso.
func (m *Maintainer) logFailure(ctx context.Context, msg string, err error, attrs ...any) {
	if errors.Is(err, context.Canceled) || ctx.Err() != nil {
		return
	}
	m.beat.Fail(m.now(), err)
	m.log.Error(msg, append(attrs, "err", err)...)
}

// utcDay devuelve la medianoche UTC del dia de t.
func utcDay(t time.Time) time.Time {
	return t.UTC().Truncate(24 * time.Hour)
}
