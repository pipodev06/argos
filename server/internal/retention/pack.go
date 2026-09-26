package retention

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"argos/server/internal/chunk"
)

const (
	// defaultPackBudget acota el trabajo de empaquetado de un ciclo: particiones que necesitan codificar. Las que ya
	// estan empaquetadas (y solo esperan su borrado) no cuentan. Un respaldo (backfill) tras actualizar avanza
	// una particion por ciclo en vez de monopolizar la BD.
	defaultPackBudget = 1
)

// packGroup es un host-dia de una particion: cuantas filas crudas tiene y cuantas dice su chunk (nil = sin chunk).
type packGroup struct {
	hostID string
	day    time.Time
	rows   int
	chunkN *int
}

// packExpired empaqueta en metrics_chunks las particiones de `metrics` que policy permite borrar. Devuelve las
// particiones que NO estan completamente empaquetadas (y por tanto no deben borrarse). Cada host-dia se codifica, se
// verifica decodificando (filas y CRC) y se guarda con un upsert idempotente; un fallo en un host no impide los demas.
// Va de la particion mas vieja a la mas nueva y solo codifica packBudget particiones por ciclo. Si no pudo ni listar
// las particiones devuelve (nil, err): el llamador no debe borrar nada.
func (m *Maintainer) packExpired(ctx context.Context, conn *pgx.Conn, policy dropPolicy) (map[string]bool, error) {
	parts, err := m.listPartitions(ctx, conn, tableMetrics)
	if err != nil {
		return nil, err
	}
	var eligible []partition
	for _, p := range parts {
		if policy.allowed(p.upper) {
			eligible = append(eligible, p)
		}
	}
	slices.SortFunc(eligible, func(a, b partition) int { return a.upper.Compare(*b.upper) })

	unpacked := map[string]bool{}
	var errs []error
	budget := m.packBudget
	for _, p := range eligible {
		if budget == 0 {
			unpacked[p.name] = true
			continue
		}
		todo, err := m.packTodo(ctx, conn, p)
		if err != nil {
			unpacked[p.name] = true
			errs = append(errs, err)
			continue
		}
		if len(todo) == 0 {
			continue // ya empaquetada: solo espera su borrado
		}
		budget--
		if err := m.packPartition(ctx, conn, p, todo); err != nil {
			unpacked[p.name] = true
			errs = append(errs, err)
		}
	}
	return unpacked, errors.Join(errs...)
}

// packTodo lista los host-dias de la particion que aun no tienen un chunk que coincida con sus filas crudas.
func (m *Maintainer) packTodo(ctx context.Context, conn *pgx.Conn, p partition) ([]packGroup, error) {
	// El nombre sale de pg_inherits (nunca de input externo) y va sanitizado como identificador.
	q := fmt.Sprintf(`
WITH g AS (
  SELECT host_id, (ts AT TIME ZONE 'UTC')::date AS day, count(*)::int AS n FROM %s GROUP BY 1, 2
)
SELECT g.host_id, g.day, g.n, c.n FROM g LEFT JOIN metrics_chunks c ON c.host_id = g.host_id AND c.day = g.day
 ORDER BY g.host_id, g.day`, pgx.Identifier{p.name}.Sanitize())
	rows, err := conn.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("buscando host-dias de %s por empaquetar: %w", p.name, err)
	}
	groups, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (packGroup, error) {
		var g packGroup
		err := r.Scan(&g.hostID, &g.day, &g.rows, &g.chunkN)
		return g, err
	})
	if err != nil {
		return nil, fmt.Errorf("leyendo host-dias de %s: %w", p.name, err)
	}
	return slices.DeleteFunc(groups, func(g packGroup) bool { return g.chunkN != nil && *g.chunkN == g.rows }), nil
}

// packPartition empaqueta los host-dias de todo. Devuelve un error (con todos los fallos juntos) si alguno no quedo guardado.
func (m *Maintainer) packPartition(ctx context.Context, conn *pgx.Conn, p partition, todo []packGroup) error {
	var errs []error
	var bytes, samples int
	for _, g := range todo {
		size, err := m.packHostDay(ctx, conn, g)
		if err != nil {
			errs = append(errs, fmt.Errorf("host %s dia %s: %w", g.hostID, g.day.Format(time.DateOnly), err))
			continue
		}
		bytes += size
		samples += g.rows
	}
	if len(errs) > 0 {
		return fmt.Errorf("empaquetando %s (%d de %d host-dias fallaron; la particion no se borra): %w",
			p.name, len(errs), len(todo), errors.Join(errs...))
	}
	m.log.Info("particion empaquetada", "partition", p.name, "host_days", len(todo), "samples", samples, "bytes", bytes)
	return nil
}

// packHostDay codifica un host-dia, verifica el blob decodificandolo y lo guarda. Devuelve los bytes guardados.
func (m *Maintainer) packHostDay(ctx context.Context, conn *pgx.Conn, g packGroup) (int, error) {
	from := g.day.UTC()
	rows, err := readDay(ctx, conn, g.hostID, from, from.AddDate(0, 0, 1))
	if err != nil {
		return 0, err
	}
	if len(rows) != g.rows {
		return 0, fmt.Errorf("se leyeron %d filas, se esperaban %d", len(rows), g.rows)
	}
	blob, err := chunk.Encode(rows)
	if err != nil {
		return 0, fmt.Errorf("codificando: %w", err)
	}
	if err := verifyBlob(blob, rows); err != nil {
		return 0, fmt.Errorf("verificando: %w", err)
	}
	_, err = conn.Exec(ctx,
		`INSERT INTO metrics_chunks (host_id, day, n, codec, data) VALUES ($1, $2::date, $3, $4, $5)
		 ON CONFLICT (host_id, day) DO UPDATE
		   SET n = EXCLUDED.n, codec = EXCLUDED.codec, data = EXCLUDED.data, packed_at = now()`,
		g.hostID, from, len(rows), int16(chunk.Version), blob)
	if err != nil {
		return 0, fmt.Errorf("guardando: %w", err)
	}
	return len(blob), nil
}

// verifyBlob decodifica el blob y comprueba que trae las mismas filas (cantidad y tiempos; el CRC lo valida Decode).
func verifyBlob(blob []byte, rows []chunk.Row) error {
	got, err := chunk.Decode(blob)
	if err != nil {
		return err
	}
	if len(got) != len(rows) {
		return fmt.Errorf("el blob trae %d filas, se esperaban %d", len(got), len(rows))
	}
	for i := range rows {
		if !got[i].TS.Equal(rows[i].TS) {
			return fmt.Errorf("la fila %d cambio de tiempo al decodificar", i)
		}
	}
	return nil
}

// readDay lee las filas de un host en [from, to), en orden de tiempo (de la tabla padre: la poda elige la particion).
func readDay(ctx context.Context, conn *pgx.Conn, hostID string, from, to time.Time) ([]chunk.Row, error) {
	rows, err := conn.Query(ctx,
		`SELECT ts, cpu_pct, mem_used_mb, mem_total_mb, net_rx_bytes, net_tx_bytes, swap_used_mb, swap_total_mb,
		        load1, load5, load15, battery_pct, battery_charging
		   FROM metrics WHERE host_id = $1 AND ts >= $2 AND ts < $3 ORDER BY ts`, hostID, from, to)
	if err != nil {
		return nil, fmt.Errorf("leyendo filas: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (chunk.Row, error) {
		var c chunk.Row
		err := r.Scan(&c.TS, &c.CPUPct, &c.MemUsedMB, &c.MemTotalMB, &c.NetRxBytes, &c.NetTxBytes, &c.SwapUsedMB, &c.SwapTotalMB,
			&c.Load1, &c.Load5, &c.Load15, &c.BatteryPct, &c.BatteryCharging)
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("leyendo filas: %w", err)
	}
	return out, nil
}
