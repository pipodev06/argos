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

// diskHotDays es la ventana caliente fija de disk_samples: cuanto se conserva la particion cruda antes de
// empaquetarla en disk_chunks. A diferencia de metrics.HotDays no es configurable (disk_samples no tiene
// ventana caliente como funcion expuesta al usuario, por decision del ticket E3): se fija al mismo minimo que
// exige ARGOS_DISK_RETENTION_DAYS (minRawRetentionDays en internal/config, 8), que ya cubre con margen los 7
// dias de desfase que admite el ingest (maxTimestampAge en internal/httpapi). Si DiskDays == diskHotDays (el
// piso) el empaquetado es un no-op: la cruda se borra sin dejar ventana comprimida, igual que antes de este
// cambio. Con el default (30) los dias 8..30 quedan comprimidos en vez de borrados.
const diskHotDays = 8

// packDiskExpired empaqueta en disk_chunks las particiones de disk_samples que policy permite borrar. Mismo
// contrato que packExpired (metrics): devuelve las particiones aun no completamente empaquetadas (no deben
// borrarse) y solo codifica packBudget particiones por ciclo, de la mas vieja a la mas nueva.
func (m *Maintainer) packDiskExpired(ctx context.Context, conn *pgx.Conn, policy dropPolicy) (map[string]bool, error) {
	parts, err := m.listPartitions(ctx, conn, tableDiskSamples)
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
		todo, err := m.diskPackTodo(ctx, conn, p)
		if err != nil {
			unpacked[p.name] = true
			errs = append(errs, err)
			continue
		}
		if len(todo) == 0 {
			continue
		}
		budget--
		if err := m.packDiskPartition(ctx, conn, p, todo); err != nil {
			unpacked[p.name] = true
			errs = append(errs, err)
		}
	}
	return unpacked, errors.Join(errs...)
}

// diskPackTodo lista los host-dias de la particion que aun no tienen un chunk que coincida con sus filas crudas
// (la cuenta suma todos los montajes del host ese dia).
func (m *Maintainer) diskPackTodo(ctx context.Context, conn *pgx.Conn, p partition) ([]packGroup, error) {
	// El nombre sale de pg_inherits (nunca de input externo) y va sanitizado como identificador.
	q := fmt.Sprintf(`
WITH g AS (
  SELECT host_id, (ts AT TIME ZONE 'UTC')::date AS day, count(*)::int AS n FROM %s GROUP BY 1, 2
)
SELECT g.host_id, g.day, g.n, c.n FROM g LEFT JOIN disk_chunks c ON c.host_id = g.host_id AND c.day = g.day
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

// packDiskPartition empaqueta los host-dias de todo. Un host que falla no impide a los demas; la particion no se
// borra si alguno quedo sin empaquetar.
func (m *Maintainer) packDiskPartition(ctx context.Context, conn *pgx.Conn, p partition, todo []packGroup) error {
	var errs []error
	var bytes, samples int
	for _, g := range todo {
		size, err := m.packDiskHostDay(ctx, conn, g)
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
	m.log.Info("particion de disco empaquetada", "partition", p.name, "host_days", len(todo), "samples", samples, "bytes", bytes)
	return nil
}

// packDiskHostDay codifica un host-dia (todos sus montajes), verifica el blob decodificandolo y lo guarda.
func (m *Maintainer) packDiskHostDay(ctx context.Context, conn *pgx.Conn, g packGroup) (int, error) {
	from := g.day.UTC()
	rows, err := readDiskDay(ctx, conn, g.hostID, from, from.AddDate(0, 0, 1))
	if err != nil {
		return 0, err
	}
	if len(rows) != g.rows {
		return 0, fmt.Errorf("se leyeron %d filas, se esperaban %d", len(rows), g.rows)
	}
	blob, mounts, err := chunk.EncodeDisk(rows)
	if err != nil {
		return 0, fmt.Errorf("codificando: %w", err)
	}
	if err := verifyDiskBlob(blob, rows); err != nil {
		return 0, fmt.Errorf("verificando: %w", err)
	}
	_, err = conn.Exec(ctx,
		`INSERT INTO disk_chunks (host_id, day, mount_count, n, codec, data) VALUES ($1, $2::date, $3, $4, $5, $6)
		 ON CONFLICT (host_id, day) DO UPDATE
		   SET mount_count = EXCLUDED.mount_count, n = EXCLUDED.n, codec = EXCLUDED.codec, data = EXCLUDED.data, packed_at = now()`,
		g.hostID, from, mounts, len(rows), int16(chunk.DiskVersion), blob)
	if err != nil {
		return 0, fmt.Errorf("guardando: %w", err)
	}
	return len(blob), nil
}

// verifyDiskBlob decodifica el blob y comprueba que trae las mismas filas (cantidad y tiempos; el CRC lo valida Decode).
func verifyDiskBlob(blob []byte, rows []chunk.DiskRow) error {
	got, err := chunk.DecodeDisk(blob)
	if err != nil {
		return err
	}
	if len(got) != len(rows) {
		return fmt.Errorf("el blob trae %d filas, se esperaban %d", len(got), len(rows))
	}
	return nil
}

// readDiskDay lee las filas de disco de un host en [from, to), por montaje y luego por tiempo (de la tabla
// padre: la poda elige la particion).
func readDiskDay(ctx context.Context, conn *pgx.Conn, hostID string, from, to time.Time) ([]chunk.DiskRow, error) {
	rows, err := conn.Query(ctx,
		`SELECT ts, mount, used_mb, total_mb FROM disk_samples WHERE host_id = $1 AND ts >= $2 AND ts < $3 ORDER BY mount, ts`,
		hostID, from, to)
	if err != nil {
		return nil, fmt.Errorf("leyendo filas: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (chunk.DiskRow, error) {
		var d chunk.DiskRow
		err := r.Scan(&d.TS, &d.Mount, &d.UsedMB, &d.TotalMB)
		return d, err
	})
	if err != nil {
		return nil, fmt.Errorf("leyendo filas: %w", err)
	}
	return out, nil
}

// trimDiskChunks borra los chunks de disco vencidos: un host-dia vence cuando su dia es anterior a hoy - DiskDays
// (retencion global unica, sin per-host/grupo: eso no aplica a disk_samples). Se evalua en cada ciclo.
func (m *Maintainer) trimDiskChunks(ctx context.Context, conn *pgx.Conn, now time.Time) error {
	today := utcDay(now)
	cutoff := today.AddDate(0, 0, -m.cfg.DiskDays)
	total := 0
	for {
		tag, err := conn.Exec(ctx,
			`DELETE FROM disk_chunks WHERE ctid = ANY (ARRAY(SELECT ctid FROM disk_chunks WHERE day < $1::date LIMIT $2))`,
			cutoff, chunkTrimBatch)
		if err != nil {
			return fmt.Errorf("recortando disk_chunks: %w", err)
		}
		n := int(tag.RowsAffected())
		total += n
		if n < chunkTrimBatch || ctx.Err() != nil {
			break
		}
	}
	if total > 0 {
		m.log.Info("detalle de disco vencido recortado", "table", "disk_chunks", "host_days", total, "disk_days", m.cfg.DiskDays)
	}
	return nil
}
