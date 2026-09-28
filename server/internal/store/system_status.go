package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// DeadChannel cuenta las entregas `dead` de un canal en la ventana consultada.
type DeadChannel struct {
	ChannelID string
	Name      string
	// GroupID y GroupName son el dueño del canal (nil = canal global del admin).
	GroupID   *string
	GroupName *string
	Count     int
}

// SystemStats son las cifras de la BD que alimentan GET /api/v1/system/status.
type SystemStats struct {
	// DBSizeBytes es pg_database_size de la BD actual.
	DBSizeBytes int64
	// QueuePending son las entregas `pending` (incluye las que esperan un reintento con backoff).
	QueuePending int
	// QueueOldestDue es el next_attempt_at mas antiguo entre las pendientes (nil = cola vacia): si ya paso hace
	// rato, nadie esta enviando.
	QueueOldestDue *time.Time
	// Dead son las entregas `dead` muertas desde `since`, por canal (solo canales con al menos una).
	Dead []DeadChannel
}

// SystemStats lee las cifras de auto-monitoreo. dead se cuenta por dead_at (las entregas muertas antes de la migracion
// 00035 llevan created_at como fecha).
func (s *Store) SystemStats(ctx context.Context, deadSince time.Time) (SystemStats, error) {
	var st SystemStats
	if err := s.pool.QueryRow(ctx,
		`SELECT pg_database_size(current_database()),
		        (SELECT count(*) FROM notification_deliveries WHERE status = 'pending'),
		        (SELECT min(next_attempt_at) FROM notification_deliveries WHERE status = 'pending')`).
		Scan(&st.DBSizeBytes, &st.QueuePending, &st.QueueOldestDue); err != nil {
		return SystemStats{}, fmt.Errorf("leyendo estado del sistema: %w", err)
	}
	rows, err := s.pool.Query(ctx,
		`SELECT c.id, c.name, c.group_id, g.name, count(*)
		   FROM notification_deliveries d
		   JOIN notification_channels c ON c.id = d.channel_id
		   LEFT JOIN host_groups g ON g.id = c.group_id
		  WHERE d.status = 'dead' AND d.dead_at >= $1
		  GROUP BY c.id, c.name, c.group_id, g.name
		  ORDER BY count(*) DESC, c.name
		  LIMIT 50`, deadSince)
	if err != nil {
		return SystemStats{}, fmt.Errorf("leyendo entregas dead por canal: %w", err)
	}
	st.Dead, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (DeadChannel, error) {
		var d DeadChannel
		err := row.Scan(&d.ChannelID, &d.Name, &d.GroupID, &d.GroupName, &d.Count)
		return d, err
	})
	if err != nil {
		return SystemStats{}, fmt.Errorf("leyendo entregas dead por canal: %w", err)
	}
	return st, nil
}

// FleetCounts son agregados globales de toda la flota (sin ambito de grupo) para GET /metrics.
type FleetCounts struct {
	// Hosts cuenta todos los hosts (agentes y dispositivos) por estado.
	Hosts StatusCounts
	// AlertsOpen cuenta las alertas no resueltas por severidad (info, warn, critical siempre presentes).
	AlertsOpen map[string]int
}

// FleetCounts lee los conteos globales de hosts por estado y de alertas abiertas por severidad.
func (s *Store) FleetCounts(ctx context.Context) (FleetCounts, error) {
	fc := FleetCounts{AlertsOpen: map[string]int{"info": 0, "warn": 0, "critical": 0}}
	rows, err := s.pool.Query(ctx, `SELECT status, count(*)::int FROM hosts GROUP BY status`)
	if err != nil {
		return FleetCounts{}, fmt.Errorf("contando hosts por estado: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return FleetCounts{}, fmt.Errorf("leyendo conteo de hosts: %w", err)
		}
		fc.Hosts.add(status, n)
	}
	if err := rows.Err(); err != nil {
		return FleetCounts{}, fmt.Errorf("iterando conteo de hosts: %w", err)
	}
	rows.Close()

	arows, err := s.pool.Query(ctx, `SELECT severity, count(*)::int FROM alerts WHERE state <> 'resolved' GROUP BY severity`)
	if err != nil {
		return FleetCounts{}, fmt.Errorf("contando alertas abiertas: %w", err)
	}
	defer arows.Close()
	for arows.Next() {
		var sev string
		var n int
		if err := arows.Scan(&sev, &n); err != nil {
			return FleetCounts{}, fmt.Errorf("leyendo conteo de alertas: %w", err)
		}
		fc.AlertsOpen[sev] = n
	}
	if err := arows.Err(); err != nil {
		return FleetCounts{}, fmt.Errorf("iterando conteo de alertas: %w", err)
	}
	return fc, nil
}
