package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"argos/server/internal/chunk"
)

// Retencion del detalle de metricas: host ?? grupo ?? global. El mantenimiento (paquete retention) aplica la misma
// regla en SQL (COALESCE(host, grupo, global)); un test los mantiene de acuerdo.
const (
	// DefaultDetailDays, DefaultHotDays y DefaultHourlyDays son los valores por defecto (ver internal/config).
	DefaultDetailDays = 30
	DefaultHotDays    = 8
	DefaultHourlyDays = 400

	// MinDetailDays y MaxDetailDays acotan la retencion de detalle de un host, grupo o global (CHECK de 00033).
	MinDetailDays = 8
	MaxDetailDays = 730

	// EstChunkBytes es lo que se estima que pesa un chunk (host-dia): ~23 KB medidos con metricas reales de un host
	// (root_home, una muestra cada 30 s = 2880 filas; ~8 B por muestra). Solo sirve pa avisar cuanto se perderia.
	EstChunkBytes = 23 * 1024

	settingDetailDays = "metrics_detail_days"
)

// Origen del valor efectivo de la retencion.
const (
	SourceDB      = "db"      // global fijado desde la API (tabla settings)
	SourceEnv     = "env"     // global de ARGOS_METRICS_RETENTION_DAYS
	SourceDefault = "default" // global por defecto
	SourceHost    = "host"
	SourceGroup   = "group"
	SourceGlobal  = "global"
)

// RetentionDefaults son los valores de arranque (del entorno) de la retencion de metricas.
type RetentionDefaults struct {
	// DetailDays es el detalle global cuando settings no tiene valor; DetailSource dice si vino del entorno o es el default.
	DetailDays   int
	DetailSource string
	HotDays      int
	HourlyDays   int
}

// DefaultRetention son los valores de fabrica.
func DefaultRetention() RetentionDefaults {
	return RetentionDefaults{DetailDays: DefaultDetailDays, DetailSource: SourceDefault, HotDays: DefaultHotDays, HourlyDays: DefaultHourlyDays}
}

// WithRetention devuelve un Store con los valores de arranque de retencion dados (los de la configuracion).
func (s *Store) WithRetention(d RetentionDefaults) *Store {
	c := *s
	c.retention = d
	return &c
}

// RetentionSettings es la retencion global vigente.
type RetentionSettings struct {
	DetailDays int
	// Source es db, env o default.
	Source     string
	HotDays    int
	HourlyDays int
}

// EffectiveDetail resuelve la retencion de detalle: host, si no grupo, si no el global. source es host, group o global.
func EffectiveDetail(hostDays, groupDays *int, global int) (days int, source string) {
	switch {
	case hostDays != nil:
		return *hostDays, SourceHost
	case groupDays != nil:
		return *groupDays, SourceGroup
	default:
		return global, SourceGlobal
	}
}

// globalDetail lee el detalle global: la fila de settings si existe, si no el valor de arranque.
func (s *Store) globalDetail(ctx context.Context) (days int, source string, err error) {
	var raw []byte
	err = s.pool.QueryRow(ctx, `SELECT value FROM settings WHERE key = $1`, settingDetailDays).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.retention.DetailDays, s.retention.DetailSource, nil
	}
	if err != nil {
		return 0, "", fmt.Errorf("leyendo %s: %w", settingDetailDays, err)
	}
	if err := json.Unmarshal(raw, &days); err != nil || days < MinDetailDays || days > MaxDetailDays {
		return 0, "", fmt.Errorf("valor invalido de %s en settings: %s", settingDetailDays, raw)
	}
	return days, SourceDB, nil
}

// RetentionSettings devuelve la retencion global vigente.
func (s *Store) RetentionSettings(ctx context.Context) (RetentionSettings, error) {
	days, src, err := s.globalDetail(ctx)
	if err != nil {
		return RetentionSettings{}, err
	}
	return RetentionSettings{DetailDays: days, Source: src, HotDays: s.retention.HotDays, HourlyDays: s.retention.HourlyDays}, nil
}

// SetGlobalDetailDays fija el detalle global (rige al siguiente ciclo del mantenimiento, sin reiniciar). userID vacio = sin autor.
func (s *Store) SetGlobalDetailDays(ctx context.Context, days int, userID string) error {
	var by *string
	if userID != "" {
		by = &userID
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO settings (key, value, updated_by) VALUES ($1, to_jsonb($2::int), $3)
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now(), updated_by = EXCLUDED.updated_by`,
		settingDetailDays, days, by)
	if err != nil {
		return fmt.Errorf("guardando %s: %w", settingDetailDays, err)
	}
	return nil
}

// SetHostDetailDays fija (o quita, con nil) la retencion propia de un host con agente. ErrNotFound si no existe.
func (s *Store) SetHostDetailDays(ctx context.Context, hostID string, days *int) error {
	tag, err := s.pool.Exec(ctx, `UPDATE hosts SET detail_retention_days = $2 WHERE id = $1 AND kind = 'agent'`, hostID, days)
	if err != nil {
		return fmt.Errorf("guardando la retencion del host: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// EffectiveDetailDays devuelve la retencion de detalle vigente de un host y de donde sale (host, group o global).
// ErrNotFound si el host no existe.
func (s *Store) EffectiveDetailDays(ctx context.Context, hostID string) (days int, source string, err error) {
	var hostDays, groupDays *int
	err = s.pool.QueryRow(ctx,
		`SELECT h.detail_retention_days, g.detail_retention_days
		   FROM hosts h LEFT JOIN host_groups g ON g.id = h.group_id WHERE h.id = $1`, hostID).Scan(&hostDays, &groupDays)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", ErrNotFound
	}
	if err != nil {
		return 0, "", fmt.Errorf("leyendo la retencion del host: %w", err)
	}
	global, _, err := s.globalDetail(ctx)
	if err != nil {
		return 0, "", err
	}
	days, source = EffectiveDetail(hostDays, groupDays, global)
	return days, source, nil
}

// Blanco de un cambio de retencion.
const (
	TargetGlobal = "global"
	TargetGroup  = "group"
	TargetHost   = "host"
)

// RetentionTarget es donde se aplica un cambio de retencion: global, un grupo (ID) o un host (ID).
type RetentionTarget struct {
	Kind string
	ID   string
}

// ShrinkPreview es lo que se perderia al reducir una retencion.
type ShrinkPreview struct {
	// DaysLost son los dias que se acortan (retencion actual - nueva); 0 si no se reduce nada.
	DaysLost int
	// Chunks son los host-dias de detalle que existen hoy y se borrarian; EstBytes su peso estimado.
	Chunks   int
	EstBytes int64
}

// PreviewRetention calcula que se borraria si la retencion del blanco pasara a newDays (nil = heredar). Solo cuenta a
// los hosts que usan ese valor (los que tienen uno propio mas especifico no cambian). today es "hoy" (UTC).
// ErrNotFound si el grupo o el host no existen.
func (s *Store) PreviewRetention(ctx context.Context, t RetentionTarget, newDays *int, today time.Time) (ShrinkPreview, error) {
	global, _, err := s.globalDetail(ctx)
	if err != nil {
		return ShrinkPreview{}, err
	}
	var oldEff, newEff int
	var affected string // condicion sobre h (hosts) y g (su grupo)
	var args []any
	switch t.Kind {
	case TargetGlobal:
		if newDays == nil {
			return ShrinkPreview{}, errors.New("el global no puede heredar")
		}
		oldEff, newEff = global, *newDays
		affected = `h.detail_retention_days IS NULL AND g.detail_retention_days IS NULL`
	case TargetGroup:
		var cur *int
		if err := s.pool.QueryRow(ctx, `SELECT detail_retention_days FROM host_groups WHERE id = $1`, t.ID).Scan(&cur); errors.Is(err, pgx.ErrNoRows) {
			return ShrinkPreview{}, ErrNotFound
		} else if err != nil {
			return ShrinkPreview{}, fmt.Errorf("leyendo el grupo: %w", err)
		}
		oldEff, _ = EffectiveDetail(nil, cur, global)
		newEff, _ = EffectiveDetail(nil, newDays, global)
		affected, args = `h.group_id = $3 AND h.detail_retention_days IS NULL`, []any{t.ID}
	case TargetHost:
		var hostDays, groupDays *int
		if err := s.pool.QueryRow(ctx,
			`SELECT h.detail_retention_days, g.detail_retention_days FROM hosts h LEFT JOIN host_groups g ON g.id = h.group_id
			  WHERE h.id = $1 AND h.kind = 'agent'`, t.ID).Scan(&hostDays, &groupDays); errors.Is(err, pgx.ErrNoRows) {
			return ShrinkPreview{}, ErrNotFound
		} else if err != nil {
			return ShrinkPreview{}, fmt.Errorf("leyendo el host: %w", err)
		}
		oldEff, _ = EffectiveDetail(hostDays, groupDays, global)
		newEff, _ = EffectiveDetail(newDays, groupDays, global)
		affected, args = `h.id = $3`, []any{t.ID}
	default:
		return ShrinkPreview{}, fmt.Errorf("blanco de retencion desconocido %q", t.Kind)
	}
	if newEff >= oldEff {
		return ShrinkPreview{}, nil
	}

	day := today.UTC().Truncate(24 * time.Hour)
	var p ShrinkPreview
	p.DaysLost = oldEff - newEff
	// Se borran los chunks con day < hoy - nueva; los anteriores a hoy - actual ya estaban fuera.
	err = s.pool.QueryRow(ctx,
		`SELECT count(*)::int FROM metrics_chunks c
		   JOIN hosts h ON h.id = c.host_id LEFT JOIN host_groups g ON g.id = h.group_id
		  WHERE `+affected+` AND c.day >= $1::date AND c.day < $2::date`,
		append([]any{day.AddDate(0, 0, -oldEff), day.AddDate(0, 0, -newEff)}, args...)...).Scan(&p.Chunks)
	if err != nil {
		return ShrinkPreview{}, fmt.Errorf("contando el detalle que se perderia: %w", err)
	}
	p.EstBytes = int64(p.Chunks) * EstChunkBytes
	return p, nil
}

// DetailSample es una muestra cruda de un host (lo que devuelve DetailRange): los tipos de las columnas de metrics.
// Los flotantes (cpu, load) de los dias empaquetados vienen redondeados a 2 decimales (chunk.Round2).
type DetailSample = chunk.Row

// DetailRange entrega en orden las muestras del host con from <= ts < to, sin cargarlas todas en memoria: los dias UTC
// que tienen chunk salen del chunk (uno a la vez, ~2900 muestras) y el resto de la tabla caliente metrics. Un dia con chunk
// se lee siempre del chunk (la particion cruda puede haber sido borrada). Si fn devuelve un error se detiene y lo devuelve.
func (s *Store) DetailRange(ctx context.Context, hostID string, from, to time.Time, fn func(DetailSample) error) error {
	from, to = from.UTC(), to.UTC()
	if !from.Before(to) {
		return nil
	}
	firstDay, lastDay := from.Truncate(24*time.Hour), to.Add(-time.Microsecond).Truncate(24*time.Hour)

	rows, err := s.pool.Query(ctx, `SELECT day FROM metrics_chunks WHERE host_id = $1 AND day >= $2::date AND day <= $3::date ORDER BY day`,
		hostID, firstDay, lastDay)
	if err != nil {
		return fmt.Errorf("listando los dias empaquetados: %w", err)
	}
	chunkDays, err := pgx.CollectRows(rows, pgx.RowTo[time.Time])
	if err != nil {
		return fmt.Errorf("leyendo los dias empaquetados: %w", err)
	}

	// Recorre el rango alternando tramos crudos (dias sin chunk) y chunks, siempre en orden de tiempo.
	cursor := from // desde donde falta entregar
	for _, d := range chunkDays {
		day := d.UTC().Truncate(24 * time.Hour)
		if day.After(cursor) {
			if err := s.rawRange(ctx, hostID, cursor, minTime(day, to), fn); err != nil {
				return err
			}
		}
		if err := s.chunkRange(ctx, hostID, day, from, to, fn); err != nil {
			return err
		}
		cursor = day.AddDate(0, 0, 1)
	}
	if cursor.Before(to) {
		return s.rawRange(ctx, hostID, cursor, to, fn)
	}
	return nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// rawRange entrega las filas crudas de [from, to) en orden.
func (s *Store) rawRange(ctx context.Context, hostID string, from, to time.Time, fn func(DetailSample) error) error {
	rows, err := s.pool.Query(ctx,
		`SELECT ts, cpu_pct, mem_used_mb, mem_total_mb, net_rx_bytes, net_tx_bytes, swap_used_mb, swap_total_mb,
		        load1, load5, load15, battery_pct, battery_charging
		   FROM metrics WHERE host_id = $1 AND ts >= $2 AND ts < $3 ORDER BY ts`, hostID, from, to)
	if err != nil {
		return fmt.Errorf("leyendo el detalle crudo: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r DetailSample
		if err := rows.Scan(&r.TS, &r.CPUPct, &r.MemUsedMB, &r.MemTotalMB, &r.NetRxBytes, &r.NetTxBytes, &r.SwapUsedMB, &r.SwapTotalMB,
			&r.Load1, &r.Load5, &r.Load15, &r.BatteryPct, &r.BatteryCharging); err != nil {
			return fmt.Errorf("leyendo una muestra cruda: %w", err)
		}
		r.TS = r.TS.UTC()
		if err := fn(r); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterando el detalle crudo: %w", err)
	}
	return nil
}

// chunkRange decodifica el chunk del dia y entrega las muestras de [from, to).
func (s *Store) chunkRange(ctx context.Context, hostID string, day, from, to time.Time, fn func(DetailSample) error) error {
	var data []byte
	err := s.pool.QueryRow(ctx, `SELECT data FROM metrics_chunks WHERE host_id = $1 AND day = $2::date`, hostID, day).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // lo borro el mantenimiento entre el listado y la lectura
	}
	if err != nil {
		return fmt.Errorf("leyendo el chunk del %s: %w", day.Format(time.DateOnly), err)
	}
	rows, err := chunk.Decode(data)
	if err != nil {
		return fmt.Errorf("decodificando el chunk del %s: %w", day.Format(time.DateOnly), err)
	}
	for _, r := range rows {
		if r.TS.Before(from) || !r.TS.Before(to) {
			continue
		}
		if err := fn(r); err != nil {
			return err
		}
	}
	return nil
}
