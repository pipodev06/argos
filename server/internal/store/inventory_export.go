package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Exportar el inventario (hosts, dispositivos y parches) como CSV: mismo filtro que las listas equivalentes,
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

// ExportDevices devuelve los dispositivos que cumplen f (mismo filtro que ListDevices: Q, Statuses, GroupIDs,
// NoGroup), ordenados por hostname, sin paginar y topados en maxInventoryExportRows.
func (s *Store) ExportDevices(ctx context.Context, scope Scope, f HostFilter) ([]Device, error) {
	whereSQL, args := deviceFilterWhere(scope, f)
	rows, err := s.pool.Query(ctx, deviceSelect+` WHERE `+whereSQL+
		fmt.Sprintf(` ORDER BY lower(h.hostname), h.id LIMIT %d`, maxInventoryExportRows), []any(args)...)
	if err != nil {
		return nil, fmt.Errorf("exportando dispositivos: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Device, error) { return scanDevice(row) })
	if err != nil {
		return nil, fmt.Errorf("leyendo dispositivos exportados: %w", err)
	}
	return out, nil
}

// ExportUpdatesFleet devuelve los hosts con foto de parches que cumplen f (mismo filtro que ListUpdatesFleet),
// ordenados por hostname, sin paginar y topados en maxInventoryExportRows.
func (s *Store) ExportUpdatesFleet(ctx context.Context, scope Scope, f UpdatesFleetFilter) ([]UpdatesFleetRow, error) {
	whereSQL, args := updatesFleetFilterWhere(scope, f)
	query := fmt.Sprintf(`
SELECT h.id, h.hostname, h.status, g.id AS group_id, g.name AS group_name, COALESCE(h.os, '') AS os,
       hi.os_name, hi.os_version,
       u.rev, u.pending_total, u.pending_security, u.reboot_required, u.reboot_since, u.security_pending_since,
       u.last_installed, u.source, u.cache_age_s, u.advisories, u.advisories_truncated, u.collected_at, u.received_at,
       eo.family, eo.cycle, eo.label, eo.eol_date, eo.lts, eo.eol_source
  FROM updates_current u
  JOIN hosts h ON h.id = u.host_id
  LEFT JOIN host_groups g ON g.id = h.group_id
  LEFT JOIN host_info hi ON hi.host_id = h.id
  LEFT JOIN LATERAL (
    SELECT e.family, e.cycle, e.label, e.eol_date, e.lts, e.source AS eol_source
      FROM os_eol_key(h.os, hi.os_name, hi.os_version) k
      JOIN os_eol e ON e.family = k.family AND e.cycle = k.cycle) eo ON true
 WHERE %s
 ORDER BY lower(h.hostname), h.id
 LIMIT %d`, whereSQL, maxInventoryExportRows)

	rows, err := s.pool.Query(ctx, query, []any(args)...)
	if err != nil {
		return nil, fmt.Errorf("exportando flota de parches: %w", err)
	}
	defer rows.Close()

	var out []UpdatesFleetRow
	for rows.Next() {
		var r UpdatesFleetRow
		var groupID, groupName, family, cycle, label, eolSource *string
		var eolDate *time.Time
		var lts *bool
		if err := rows.Scan(&r.HostID, &r.Hostname, &r.Status, &groupID, &groupName, &r.OS, &r.OSName, &r.OSVersion,
			&r.Updates.Rev, &r.Updates.PendingTotal, &r.Updates.PendingSecurity, &r.Updates.RebootRequired, &r.Updates.RebootSince,
			&r.Updates.SecurityPendingSince, &r.Updates.LastInstalled, &r.Updates.Source, &r.Updates.CacheAgeS, &r.Updates.Advisories,
			&r.Updates.AdvisoriesTruncated, &r.Updates.CollectedAt, &r.Updates.ReceivedAt,
			&family, &cycle, &label, &eolDate, &lts, &eolSource); err != nil {
			return nil, fmt.Errorf("leyendo host de la flota exportado: %w", err)
		}
		if groupID != nil && groupName != nil {
			r.Group = &GroupRef{ID: *groupID, Name: *groupName}
		}
		if family != nil && cycle != nil && label != nil && eolDate != nil && lts != nil && eolSource != nil {
			r.EOL = &OSEOL{Family: *family, Cycle: *cycle, Label: *label, EOLDate: *eolDate, LTS: *lts, Estimated: *eolSource == "estimate"}
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterando flota exportada: %w", err)
	}
	return out, nil
}
