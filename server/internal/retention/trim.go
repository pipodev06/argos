package retention

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	// detailSettingKey es la fila de settings con el detalle global fijado desde la API (numero JSON).
	detailSettingKey = "metrics_detail_days"

	// chunkTrimBatch es cuantos chunks (host-dias) borra cada sentencia.
	chunkTrimBatch = 5000
)

// globalDetailDays devuelve el detalle global vigente: settings si tiene valor valido, si no el de arranque (cfg).
func (m *Maintainer) globalDetailDays(ctx context.Context, conn *pgx.Conn) (int, error) {
	var raw []byte
	err := conn.QueryRow(ctx, `SELECT value FROM settings WHERE key = $1`, detailSettingKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return m.cfg.DetailDays, nil
	}
	if err != nil {
		return 0, fmt.Errorf("leyendo %s: %w", detailSettingKey, err)
	}
	var days int
	if err := json.Unmarshal(raw, &days); err != nil || days < 1 {
		// Un valor ilegible no debe borrar mas de lo debido: se usa el de arranque y se avisa.
		m.log.Warn("valor invalido de settings: se usa la retencion de detalle de arranque", "key", detailSettingKey, "fallback_days", m.cfg.DetailDays)
		return m.cfg.DetailDays, nil
	}
	return days, nil
}

// trimChunks borra los chunks de detalle vencidos: un host-dia vence cuando su dia es anterior a hoy - N, con N la
// retencion efectiva del host (host ?? grupo ?? global, la misma regla que store.EffectiveDetail). Se evalua en cada
// ciclo (es barato: el PK cubre el recorrido y sin vencidos no escribe nada), asi un cambio de retencion rige al
// siguiente ciclo sin reiniciar. Subir la retencion vale hacia adelante (lo ya borrado no vuelve); bajarla recorta.
func (m *Maintainer) trimChunks(ctx context.Context, conn *pgx.Conn, now time.Time) error {
	global, err := m.globalDetailDays(ctx, conn)
	if err != nil {
		return err
	}
	today := utcDay(now)
	total := 0
	for {
		tag, err := conn.Exec(ctx,
			`DELETE FROM metrics_chunks
			  WHERE ctid = ANY (ARRAY(
			    SELECT c.ctid FROM metrics_chunks c
			      JOIN hosts h ON h.id = c.host_id
			      LEFT JOIN host_groups g ON g.id = h.group_id
			     WHERE c.day < $1::date - COALESCE(h.detail_retention_days, g.detail_retention_days, $2::int)
			     LIMIT $3))`,
			today, global, chunkTrimBatch)
		if err != nil {
			return fmt.Errorf("recortando metrics_chunks: %w", err)
		}
		n := int(tag.RowsAffected())
		total += n
		if n < chunkTrimBatch || ctx.Err() != nil {
			break
		}
	}
	if total > 0 {
		m.log.Info("detalle vencido recortado", "table", "metrics_chunks", "host_days", total, "global_days", global)
	}
	return nil
}
