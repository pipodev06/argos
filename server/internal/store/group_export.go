package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Exportar el cliente antes de purgarlo (B17): un volcado de sus hosts/dispositivos y su disponibilidad, para
// conservar un respaldo legible fuera de la base antes de la baja irreversible.

// maxGroupExportHosts acota el volcado de hosts.csv: un grupo real (un cliente) nunca se acerca a este numero;
// evita cargar en memoria un volcado descontrolado si algun dia se reutiliza para un ambito mayor.
const maxGroupExportHosts = 5000

// GroupExportHost es una fila de hosts.csv: un host o dispositivo del grupo.
type GroupExportHost struct {
	ID, Hostname, Kind, Status string
	OS, IP                     *string
	CreatedAt                  time.Time
}

// GroupName devuelve el nombre del grupo; ErrNotFound si no existe.
func (s *Store) GroupName(ctx context.Context, groupID string) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, `SELECT name FROM host_groups WHERE id = $1`, groupID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("leyendo grupo: %w", err)
	}
	return name, nil
}

// ExportGroupHosts devuelve los hosts y dispositivos del grupo (volcado completo, sin paginar, acotado a
// maxGroupExportHosts) para hosts.csv. No es la consulta paginada de ListHosts: aqui interesa todo el grupo de una
// vez, no una pagina.
func (s *Store) ExportGroupHosts(ctx context.Context, groupID string) ([]GroupExportHost, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text, hostname, kind, status, os, address::text, created_at
	                                   FROM hosts WHERE group_id = $1 ORDER BY lower(hostname), id LIMIT $2`,
		groupID, maxGroupExportHosts)
	if err != nil {
		return nil, fmt.Errorf("listando hosts del grupo: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (GroupExportHost, error) {
		var h GroupExportHost
		err := row.Scan(&h.ID, &h.Hostname, &h.Kind, &h.Status, &h.OS, &h.IP, &h.CreatedAt)
		return h, err
	})
	if err != nil {
		return nil, fmt.Errorf("leyendo hosts del grupo: %w", err)
	}
	return out, nil
}
