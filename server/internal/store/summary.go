package store

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

// topN es el largo de los rankings y de "recientemente caidos" del resumen.
const topN = 10

// StatusCounts cuenta hosts por estado.
type StatusCounts struct {
	Up, Down, Offline, Pending, Disabled int
}

// Total suma todos los estados.
func (c StatusCounts) Total() int { return c.Up + c.Down + c.Offline + c.Pending + c.Disabled }

func (c *StatusCounts) add(status string, n int) {
	switch status {
	case "up":
		c.Up += n
	case "down":
		c.Down += n
	case "offline":
		c.Offline += n
	case "pending":
		c.Pending += n
	case "disabled":
		c.Disabled += n
	}
}

// GroupCounts son los conteos de un grupo; Group nil = hosts sin grupo.
type GroupCounts struct {
	Group  *GroupRef
	Counts StatusCounts
}

// TopHost es una fila de un ranking (solo hosts up). Mount solo en el de disco.
type TopHost struct {
	ID       string
	Hostname string
	Value    float64
	Mount    *string
}

// RecentDown es un host down (no offline) con el momento en que cayo.
type RecentDown struct {
	ID              string
	Hostname        string
	StatusChangedAt time.Time
	Group           *GroupRef
}

// Summary es la foto de la flota pa el dashboard.
type Summary struct {
	Counts StatusCounts
	// Devices cuenta los dispositivos sin agente (`kind='device'`), aparte de los hosts.
	Devices      StatusCounts
	ByGroup      []GroupCounts
	TopCPU       []TopHost
	TopMem       []TopHost
	TopDisk      []TopHost
	RecentlyDown []RecentDown
}

// Summary calcula el resumen de la flota: conteos por estado y por grupo (solo grupos con hosts, mas "sin
// grupo" si hay), los 10 hosts up con mas cpu, memoria y disco, y los 10 caidos mas recientes (solo down:
// un host offline no requiere atencion).
//
// El resumen cuenta solo hosts con agente (`kind='agent'`): los dispositivos viven en /dispositivos.
//
// scope limita TODO el resumen (conteos, rankings y caidos) a los hosts visibles.
func (s *Store) Summary(ctx context.Context, scope Scope) (Summary, error) {
	var sum Summary

	var cargs argList
	rows, err := s.pool.Query(ctx,
		`SELECT g.id, g.name, h.status, count(*)::int
		   FROM hosts h LEFT JOIN host_groups g ON g.id = h.group_id
		  WHERE h.kind = 'agent' AND `+scope.groupCond("h.group_id", &cargs)+`
		  GROUP BY g.id, g.name, h.status`, []any(cargs)...)
	if err != nil {
		return Summary{}, fmt.Errorf("contando hosts por grupo: %w", err)
	}
	defer rows.Close()
	byGroup := map[string]*GroupCounts{}
	var order []string
	for rows.Next() {
		var id, name *string
		var status string
		var n int
		if err := rows.Scan(&id, &name, &status, &n); err != nil {
			return Summary{}, fmt.Errorf("leyendo conteo: %w", err)
		}
		key := ""
		if id != nil {
			key = *id
		}
		gc, ok := byGroup[key]
		if !ok {
			gc = &GroupCounts{}
			if id != nil && name != nil {
				gc.Group = &GroupRef{ID: *id, Name: *name}
			}
			byGroup[key] = gc
			order = append(order, key)
		}
		gc.Counts.add(status, n)
		sum.Counts.add(status, n)
	}
	if err := rows.Err(); err != nil {
		return Summary{}, fmt.Errorf("iterando conteos: %w", err)
	}
	rows.Close()
	sum.ByGroup = orderedGroups(byGroup, order)

	var devArgs argList
	devRows, err := s.pool.Query(ctx,
		`SELECT h.status, count(*)::int FROM hosts h WHERE h.kind = 'device' AND `+scope.groupCond("h.group_id", &devArgs)+` GROUP BY h.status`, []any(devArgs)...)
	if err != nil {
		return Summary{}, fmt.Errorf("contando dispositivos: %w", err)
	}
	defer devRows.Close()
	for devRows.Next() {
		var status string
		var n int
		if err := devRows.Scan(&status, &n); err != nil {
			return Summary{}, fmt.Errorf("leyendo conteo de dispositivos: %w", err)
		}
		sum.Devices.add(status, n)
	}
	if err := devRows.Err(); err != nil {
		return Summary{}, fmt.Errorf("iterando dispositivos: %w", err)
	}
	devRows.Close()

	if sum.TopCPU, err = s.top(ctx, scope, `l.cpu_pct::float8`, `l.cpu_pct IS NOT NULL`, false); err != nil {
		return Summary{}, err
	}
	if sum.TopMem, err = s.top(ctx, scope, `l.mem_used_mb::float8 * 100 / l.mem_total_mb`, `l.mem_total_mb > 0`, false); err != nil {
		return Summary{}, err
	}
	if sum.TopDisk, err = s.top(ctx, scope, `l.disk_max_pct::float8`, `l.disk_max_pct IS NOT NULL`, true); err != nil {
		return Summary{}, err
	}

	dargs := argList{topN}
	down, err := s.pool.Query(ctx,
		`SELECT h.id, h.hostname, h.status_changed_at, g.id, g.name
		   FROM hosts h LEFT JOIN host_groups g ON g.id = h.group_id
		  WHERE h.kind = 'agent' AND h.status = 'down' AND `+scope.groupCond("h.group_id", &dargs)+`
		  ORDER BY h.status_changed_at DESC, h.id LIMIT $1`, []any(dargs)...)
	if err != nil {
		return Summary{}, fmt.Errorf("leyendo hosts caidos: %w", err)
	}
	defer down.Close()
	sum.RecentlyDown = []RecentDown{}
	for down.Next() {
		var r RecentDown
		var gid, gname *string
		if err := down.Scan(&r.ID, &r.Hostname, &r.StatusChangedAt, &gid, &gname); err != nil {
			return Summary{}, fmt.Errorf("leyendo host caido: %w", err)
		}
		if gid != nil && gname != nil {
			r.Group = &GroupRef{ID: *gid, Name: *gname}
		}
		r.StatusChangedAt = r.StatusChangedAt.UTC()
		sum.RecentlyDown = append(sum.RecentlyDown, r)
	}
	if err := down.Err(); err != nil {
		return Summary{}, fmt.Errorf("iterando hosts caidos: %w", err)
	}
	return sum, nil
}

// orderedGroups devuelve los grupos por nombre y "sin grupo" al final.
func orderedGroups(byGroup map[string]*GroupCounts, order []string) []GroupCounts {
	out := make([]GroupCounts, 0, len(order))
	var ungrouped *GroupCounts
	for _, k := range order {
		if gc := byGroup[k]; gc.Group == nil {
			ungrouped = gc
		} else {
			out = append(out, *gc)
		}
	}
	slices.SortFunc(out, func(a, b GroupCounts) int {
		return cmp.Or(strings.Compare(strings.ToLower(a.Group.Name), strings.ToLower(b.Group.Name)),
			strings.Compare(a.Group.ID, b.Group.ID))
	})
	if ungrouped != nil {
		out = append(out, *ungrouped)
	}
	return out
}

// top devuelve los topN hosts up con mayor valor. value y cond son fragmentos SQL fijos (nunca input externo);
// withMount agrega el montaje del disco mas lleno.
func (s *Store) top(ctx context.Context, scope Scope, value, cond string, withMount bool) ([]TopHost, error) {
	mount := "NULL::text"
	if withMount {
		mount = "l.disk_max_mount"
	}
	args := argList{topN}
	rows, err := s.pool.Query(ctx, fmt.Sprintf(
		`SELECT h.id, h.hostname, %[1]s AS value, %[2]s
		   FROM hosts h JOIN host_latest l ON l.host_id = h.id
		  WHERE h.status = 'up' AND %[3]s AND %[4]s
		  ORDER BY value DESC, h.id LIMIT $1`, value, mount, cond, scope.groupCond("h.group_id", &args)), []any(args)...)
	if err != nil {
		return nil, fmt.Errorf("leyendo ranking: %w", err)
	}
	defer rows.Close()

	out := []TopHost{}
	for rows.Next() {
		var t TopHost
		if err := rows.Scan(&t.ID, &t.Hostname, &t.Value, &t.Mount); err != nil {
			return nil, fmt.Errorf("leyendo fila del ranking: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterando ranking: %w", err)
	}
	return out, nil
}
