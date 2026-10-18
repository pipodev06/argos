package store

import (
	"context"
	"fmt"
)

// Exportar el inventario (hosts) como CSV: mismo filtro que las listas equivalentes,
// pero sin paginar. maxInventoryExportRows corta en silencio (sin error) igual que maxGroupExportHosts: un
// ambito real nunca se acerca a este numero.
const maxInventoryExportRows = 5000

// ExportHosts devuelve los hosts que cumplen f (mismo filtro que ListHosts), ordenados por hostname, sin
// paginar y topados en maxInventoryExportRows. Ignora f.Sort, f.Desc, f.Limit y f.After.
func (s *Store) ExportHosts(ctx context.Context, scope Scope, f HostFilter) ([]HostRow, error) {
	whereSQL, args := hostFilterWhere(scope, f)
	query := fmt.Sprintf(`
SELECT h.id, h.hostname, h.kind, h.status, h.status_changed_at, h.last_seen_at, g.id AS group_id, g.name AS group_name,
       COALESCE(h.os, '') AS os, hi.os_name, hi.os_version, hi.ips, hi.boot_time, NULLIF(hi.agent_version, '') AS agent_version, hi.physical_disks,
       l.cpu_pct, l.mem_used_mb, l.mem_total_mb, l.disk_max_pct, l.disk_max_mount, l.battery_pct, l.battery_charging
  FROM hosts h
  LEFT JOIN host_groups g ON g.id = h.group_id
  LEFT JOIN host_latest l ON l.host_id = h.id
  LEFT JOIN host_info hi ON hi.host_id = h.id
 WHERE %s
 ORDER BY lower(h.hostname), h.id
 LIMIT %d`, whereSQL, maxInventoryExportRows)

	rows, err := s.pool.Query(ctx, query, []any(args)...)
	if err != nil {
		return nil, fmt.Errorf("exportando hosts: %w", err)
	}
	defer rows.Close()

	var out []HostRow
	for rows.Next() {
		var h HostRow
		var groupID, groupName *string
		if err := rows.Scan(&h.ID, &h.Hostname, &h.Kind, &h.Status, &h.StatusChangedAt, &h.LastSeenAt, &groupID, &groupName, &h.OS,
			&h.OSName, &h.OSVersion, &h.IPs, &h.BootTime, &h.AgentVersion, &h.PhysicalDisks, &h.CPUPct, &h.MemUsedMB, &h.MemTotalMB,
			&h.DiskPct, &h.DiskMount, &h.BatteryPct, &h.BatteryCharging); err != nil {
			return nil, fmt.Errorf("leyendo host exportado: %w", err)
		}
		if groupID != nil && groupName != nil {
			h.Group = &GroupRef{ID: *groupID, Name: *groupName}
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterando hosts exportados: %w", err)
	}
	return out, nil
}
