package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Modulos del inventario del host.
const (
	InventoryPorts     = "ports"
	InventoryServices  = "services"
	InventoryProcesses = "processes"
)

// maxInventoryChangesPerDay es el tope de filas de cambio por host y dia (UTC). El exceso se pliega en una fila
// 'overflow'. Los dos modulos con historial se cuentan juntos y no se serializan entre si: dos envios simultaneos
// del mismo host pueden pasarse por unas pocas filas, lo que no rompe el objetivo (acotar el volumen).
const maxInventoryChangesPerDay = 1000

// ErrNeedFull: el latido no coincide con la foto guardada (o no hay foto); el agente debe reenviar el listado.
var ErrNeedFull = errors.New("se necesita el inventario completo")

// InventoryFull es el envio completo de un modulo, ya validado. Items son los elementos serializados de la lista
// blanca (sin campos prohibidos ni el pid).
type InventoryFull struct {
	Module      string
	Rev         string
	Items       []json.RawMessage
	Truncated   bool
	CollectedAt time.Time
}

// inventoryChange es una fila de inventory_changes por insertar.
type inventoryChange struct {
	kind, key     string
	before, after json.RawMessage
}

// IngestInventory guarda la foto de un modulo. La primera foto es la linea base (sin cambios); despues, si el rev
// cambio, calcula el diff contra la foto anterior (solo puertos y servicios) y lo registra, todo en una transaccion
// con la fila actual bloqueada. Reenviar el mismo rev no genera cambios (idempotente).
func (s *Store) IngestInventory(ctx context.Context, hostID string, in InventoryFull, now time.Time) error {
	items, err := json.Marshal(in.Items)
	if err != nil {
		return fmt.Errorf("serializando inventario: %w", err)
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`INSERT INTO inventory_current (host_id, module, rev, items, item_count, truncated, collected_at, received_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT (host_id, module) DO NOTHING`,
			hostID, in.Module, in.Rev, items, len(in.Items), in.Truncated, in.CollectedAt, now)
		if err != nil {
			return fmt.Errorf("insertando la foto: %w", err)
		}
		if tag.RowsAffected() == 1 {
			return nil // linea base
		}
		var oldRev string
		var oldItems []byte
		if err := tx.QueryRow(ctx,
			`SELECT rev, items FROM inventory_current WHERE host_id = $1 AND module = $2 FOR UPDATE`,
			hostID, in.Module).Scan(&oldRev, &oldItems); err != nil {
			return fmt.Errorf("leyendo la foto actual: %w", err)
		}
		if oldRev != in.Rev && in.Module != InventoryProcesses {
			var before []json.RawMessage
			if err := json.Unmarshal(oldItems, &before); err != nil {
				return fmt.Errorf("leyendo items guardados: %w", err)
			}
			changes, err := diffInventory(in.Module, before, in.Items)
			if err != nil {
				return err
			}
			if err := insertInventoryChanges(ctx, tx, hostID, in.Module, changes, now); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx,
			`UPDATE inventory_current SET rev = $3, items = $4, item_count = $5, truncated = $6, collected_at = $7, received_at = $8
			  WHERE host_id = $1 AND module = $2`,
			hostID, in.Module, in.Rev, items, len(in.Items), in.Truncated, in.CollectedAt, now); err != nil {
			return fmt.Errorf("actualizando la foto: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("guardando inventario %s: %w", in.Module, err)
	}
	return nil
}

// InventoryHeartbeat confirma que la foto no cambio (de cualquier modulo, incluido updates). ErrNeedFull si el rev no coincide o no hay foto.
func (s *Store) InventoryHeartbeat(ctx context.Context, hostID, module, rev string, now time.Time) error {
	if module == InventoryUpdates {
		return s.updatesHeartbeat(ctx, hostID, rev, now)
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE inventory_current SET received_at = $4 WHERE host_id = $1 AND module = $2 AND rev = $3`,
		hostID, module, rev, now)
	if err != nil {
		return fmt.Errorf("registrando latido de inventario: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNeedFull
	}
	return nil
}

// PurgeInventoryChanges borra cambios de inventario anteriores a before.
func (s *Store) PurgeInventoryChanges(ctx context.Context, before time.Time, batch int) (int, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM inventory_changes WHERE id IN (SELECT id FROM inventory_changes WHERE at < $1 ORDER BY at LIMIT $2)`,
		before, batch)
	if err != nil {
		return 0, fmt.Errorf("purgando cambios de inventario: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// inventoryKey es la clave de identidad de un elemento: puertos proto|bind|port, servicios name.
func inventoryKey(module string, raw json.RawMessage) (string, error) {
	var it struct {
		Proto string `json:"proto"`
		Bind  string `json:"bind"`
		Port  int    `json:"port"`
		Name  string `json:"name"`
	}
	if err := json.Unmarshal(raw, &it); err != nil {
		return "", fmt.Errorf("leyendo un elemento de %s: %w", module, err)
	}
	if module == InventoryPorts {
		return it.Proto + "|" + it.Bind + "|" + strconv.Itoa(it.Port), nil
	}
	return it.Name, nil
}

// canonicalJSON reordena las claves (json.Marshal de un map las ordena) para comparar sin importar el orden.
func canonicalJSON(raw json.RawMessage) (string, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", fmt.Errorf("leyendo un elemento: %w", err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("serializando un elemento: %w", err)
	}
	return string(b), nil
}

func indexInventory(module string, items []json.RawMessage) (map[string]json.RawMessage, error) {
	m := make(map[string]json.RawMessage, len(items))
	for _, it := range items {
		k, err := inventoryKey(module, it)
		if err != nil {
			return nil, err
		}
		m[k] = it
	}
	return m, nil
}

// diffInventory devuelve los cambios entre dos fotos, ordenados por clave.
func diffInventory(module string, before, after []json.RawMessage) ([]inventoryChange, error) {
	b, err := indexInventory(module, before)
	if err != nil {
		return nil, err
	}
	a, err := indexInventory(module, after)
	if err != nil {
		return nil, err
	}
	var out []inventoryChange
	for k, av := range a {
		bv, ok := b[k]
		if !ok {
			out = append(out, inventoryChange{kind: "added", key: k, after: av})
			continue
		}
		ca, err := canonicalJSON(av)
		if err != nil {
			return nil, err
		}
		cb, err := canonicalJSON(bv)
		if err != nil {
			return nil, err
		}
		if ca != cb {
			out = append(out, inventoryChange{kind: "changed", key: k, before: bv, after: av})
		}
	}
	for k, bv := range b {
		if _, ok := a[k]; !ok {
			out = append(out, inventoryChange{kind: "removed", key: k, before: bv})
		}
	}
	slices.SortFunc(out, func(x, y inventoryChange) int { return strings.Compare(x.key, y.key) })
	return out, nil
}

// insertInventoryChanges inserta los cambios respetando el tope diario por host: lo que no cabe se pliega en una
// sola fila 'overflow' (after = {"dropped": n}).
func insertInventoryChanges(ctx context.Context, tx pgx.Tx, hostID, module string, changes []inventoryChange, now time.Time) error {
	if len(changes) == 0 {
		return nil
	}
	var today int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM inventory_changes WHERE host_id = $1 AND at >= $2`,
		hostID, now.UTC().Truncate(24*time.Hour)).Scan(&today); err != nil {
		return fmt.Errorf("contando cambios del dia: %w", err)
	}
	room := max(maxInventoryChangesPerDay-today, 0)
	if len(changes) > room {
		keep := max(room-1, 0)
		dropped := len(changes) - keep
		changes = changes[:keep]
		if room > 0 {
			changes = append(changes, inventoryChange{kind: "overflow",
				after: json.RawMessage(`{"dropped":` + strconv.Itoa(dropped) + `}`)})
		}
	}
	if len(changes) == 0 {
		return nil
	}
	kinds := make([]string, len(changes))
	keys := make([]string, len(changes))
	befores := make([]*string, len(changes))
	afters := make([]*string, len(changes))
	for i, c := range changes {
		kinds[i], keys[i] = c.kind, c.key
		if c.before != nil {
			s := string(c.before)
			befores[i] = &s
		}
		if c.after != nil {
			s := string(c.after)
			afters[i] = &s
		}
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO inventory_changes (host_id, module, kind, key, before, after, at)
		 SELECT $1, $2, t.kind, t.key, t.before::jsonb, t.after::jsonb, $7
		   FROM unnest($3::text[], $4::text[], $5::text[], $6::text[]) AS t(kind, key, before, after)`,
		hostID, module, kinds, keys, befores, afters, now); err != nil {
		return fmt.Errorf("insertando cambios: %w", err)
	}
	return nil
}

// ValidInventoryModule dice si module es uno de los modulos del inventario.
func ValidInventoryModule(module string) bool {
	return module == InventoryPorts || module == InventoryServices || module == InventoryProcesses || module == InventoryUpdates
}
