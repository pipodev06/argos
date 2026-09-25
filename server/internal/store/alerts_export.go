package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Topes de la exportacion CSV de alertas.
const (
	MaxExportRows          = 50_000
	ExportStatementTimeout = 30 * time.Second
)

// ErrExportTooLarge: la exportacion tiene mas de MaxExportRows filas.
var ErrExportTooLarge = errors.New("exportacion demasiado grande")

// AlertExportFilter acota las alertas exportadas: abiertas en [From, To), de hosts del ambito, opcionalmente de
// un grupo y de ciertos estados (vacio = todos).
type AlertExportFilter struct {
	From, To time.Time
	GroupID  *string
	States   []string
}

// AlertExportRow es una alerta con el nombre del grupo actual de su host.
type AlertExportRow struct {
	Alert
	GroupName *string
}

// ExportAlerts entrega a fn, una a una y sin cargarlas en memoria, las alertas del filtro (mas recientes primero).
// Un COUNT previo devuelve ErrExportTooLarge antes de llamar a fn; el LIMIT de la consulta es el tope duro. Un
// grupo inexistente o fuera del ambito da ErrNotFound. Devuelve las filas entregadas (tambien si fn falla).
func (s *Store) ExportAlerts(ctx context.Context, scope Scope, f AlertExportFilter, fn func(AlertExportRow) error) (int, error) {
	var args argList
	conds := []string{"a.opened_at >= " + args.add(f.From), "a.opened_at < " + args.add(f.To), scope.groupCond("h.group_id", &args)}
	if f.GroupID != nil {
		conds = append(conds, "h.group_id = "+args.add(*f.GroupID)+"::uuid")
	}
	if len(f.States) > 0 {
		conds = append(conds, "a.state = ANY("+args.add(f.States)+")")
	}
	where := ` FROM alerts a JOIN hosts h ON h.id = a.host_id LEFT JOIN host_groups g ON g.id = h.group_id WHERE ` +
		strings.Join(conds, " AND ")

	n := 0
	err := s.reportTx(ctx, ExportStatementTimeout, f.GroupID, scope, func(tx pgx.Tx) error {
		var total int
		if err := tx.QueryRow(ctx, `SELECT count(*)::int`+where, []any(args)...).Scan(&total); err != nil {
			return fmt.Errorf("contando alertas a exportar: %w", err)
		}
		if total > MaxExportRows {
			return ErrExportTooLarge
		}
		rows, err := tx.Query(ctx, `SELECT a.id, a.rule_id, a.rule_name, a.rule_kind, a.metric, a.threshold, a.severity, a.state, a.opened_at,
       a.trigger_value, a.peak_value, a.suppressed, a.host_id, h.hostname,
       a.acknowledged_by_name, a.acknowledged_at, a.resolved_at, a.resolved_by_name, a.resolve_reason, g.name`+where+
			fmt.Sprintf(` ORDER BY a.opened_at DESC, a.id DESC LIMIT %d`, MaxExportRows), []any(args)...)
		if err != nil {
			return fmt.Errorf("consultando alertas a exportar: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var r AlertExportRow
			a := &r.Alert
			if err := rows.Scan(&a.ID, &a.RuleID, &a.RuleName, &a.RuleKind, &a.Metric, &a.Threshold, &a.Severity, &a.State, &a.OpenedAt,
				&a.TriggerValue, &a.PeakValue, &a.Suppressed, &a.HostID, &a.Hostname,
				&a.AcknowledgedByName, &a.AcknowledgedAt, &a.ResolvedAt, &a.ResolvedByName, &a.ResolveReason, &r.GroupName); err != nil {
				return fmt.Errorf("leyendo alerta a exportar: %w", err)
			}
			if err := fn(r); err != nil {
				return err
			}
			n++
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterando alertas a exportar: %w", err)
		}
		return nil
	})
	return n, err
}
