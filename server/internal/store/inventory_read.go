package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// InventoryMeta describe la ultima foto de un modulo sin sus elementos.
type InventoryMeta struct {
	Module      string
	Count       int
	Truncated   bool
	CollectedAt time.Time // cuando el agente tomo la foto
	ReceivedAt  time.Time // ultimo envio o latido recibido (frescura de la foto)
}

// InventoryOverview devuelve los modulos que el host ha reportado alguna vez, ordenados por nombre.
// ErrNotFound si el host no existe o esta fuera del ambito.
func (s *Store) InventoryOverview(ctx context.Context, scope Scope, hostID string) ([]InventoryMeta, error) {
	if err := s.requireHost(ctx, scope, hostID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT module, item_count, truncated, collected_at, received_at
		   FROM inventory_current WHERE host_id = $1 ORDER BY module`, hostID)
	if err != nil {
		return nil, fmt.Errorf("listando modulos de inventario: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (InventoryMeta, error) {
		var m InventoryMeta
		err := row.Scan(&m.Module, &m.Count, &m.Truncated, &m.CollectedAt, &m.ReceivedAt)
		return m, err
	})
	if err != nil {
		return nil, fmt.Errorf("leyendo modulos de inventario: %w", err)
	}
	return out, nil
}

// InventoryItems devuelve los elementos de la ultima foto de un modulo. ErrNotFound si el host no existe o esta
// fuera del ambito; una lista vacia si el modulo nunca reporto.
func (s *Store) InventoryItems(ctx context.Context, scope Scope, hostID, module string) ([]json.RawMessage, error) {
	if err := s.requireHost(ctx, scope, hostID); err != nil {
		return nil, err
	}
	var raw []byte
	err := s.pool.QueryRow(ctx,
		`SELECT items FROM inventory_current WHERE host_id = $1 AND module = $2`, hostID, module).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return []json.RawMessage{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("leyendo elementos de inventario: %w", err)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("decodificando elementos de inventario: %w", err)
	}
	return items, nil
}

// InventoryChange es una fila del historial de cambios de puertos o servicios.
type InventoryChange struct {
	ID            int64
	Module        string
	Kind          string
	Key           string
	Before, After json.RawMessage // nil si no aplica
	At            time.Time
}

// InventoryChangePage es una pagina de cambios, los mas recientes primero.
type InventoryChangePage struct {
	Items      []InventoryChange
	NextCursor *EventCursor
}

// ListInventoryChanges pagina el historial de cambios de un host (mas recientes primero). module y kind vacios =
// todos. ErrNotFound si el host no existe o esta fuera del ambito. limit debe estar entre 1 y MaxAlertListLimit.
func (s *Store) ListInventoryChanges(ctx context.Context, scope Scope, hostID, module, kind string, after *EventCursor, limit int) (InventoryChangePage, error) {
	if err := s.requireHost(ctx, scope, hostID); err != nil {
		return InventoryChangePage{}, err
	}
	sql := `SELECT id, module, kind, key, before, after, at FROM inventory_changes WHERE host_id = $1`
	args := argList{hostID}
	if module != "" {
		sql += ` AND module = ` + args.add(module)
	}
	if kind != "" {
		sql += ` AND kind = ` + args.add(kind)
	}
	if after != nil {
		sql += fmt.Sprintf(` AND (at, id) < (%s::timestamptz, %s::bigint)`, args.add(after.At), args.add(after.ID))
	}
	sql += fmt.Sprintf(` ORDER BY at DESC, id DESC LIMIT %d`, limit+1)

	rows, err := s.pool.Query(ctx, sql, []any(args)...)
	if err != nil {
		return InventoryChangePage{}, fmt.Errorf("listando cambios de inventario: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (InventoryChange, error) {
		var c InventoryChange
		err := row.Scan(&c.ID, &c.Module, &c.Kind, &c.Key, &c.Before, &c.After, &c.At)
		return c, err
	})
	if err != nil {
		return InventoryChangePage{}, fmt.Errorf("leyendo cambios de inventario: %w", err)
	}
	page := InventoryChangePage{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		last := page.Items[limit-1]
		page.NextCursor = &EventCursor{At: last.At, ID: last.ID}
	}
	if page.Items == nil {
		page.Items = []InventoryChange{}
	}
	return page, nil
}
