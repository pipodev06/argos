package store

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Limites de la lista de hosts.
const (
	DefaultHostListLimit = 50
	MaxHostListLimit     = 100
)

// hostSort define un orden de la lista: expr es la expresion SQL del valor (puede ser NULL; los hosts sin
// valor van siempre al final), cast su tipo y neutral el valor que ocupa el lugar del NULL (irrelevante pa
// el orden porque el flag de NULL va primero, solo mantiene la clave sin NULLs).
type hostSort struct {
	expr        string
	cast        string
	neutral     string
	defaultDesc bool
}

// hostSorts es la lista blanca de ordenes: los fragmentos SQL salen de aqui, nunca del cliente.
var hostSorts = map[string]hostSort{
	"severity": {expr: `CASE h.status WHEN 'down' THEN 0 WHEN 'pending' THEN 1 WHEN 'offline' THEN 2 WHEN 'up' THEN 3 ELSE 4 END`,
		cast: "int", neutral: "0"},
	"name":      {expr: `lower(h.hostname)`, cast: "text", neutral: "''"},
	"status":    {expr: `h.status`, cast: "text", neutral: "''"},
	"group":     {expr: `lower(g.name)`, cast: "text", neutral: "''"},
	"cpu":       {expr: `l.cpu_pct::float8`, cast: "float8", neutral: "0", defaultDesc: true},
	"mem":       {expr: `l.mem_used_mb::float8 / NULLIF(l.mem_total_mb, 0)`, cast: "float8", neutral: "0", defaultDesc: true},
	"disk":      {expr: `l.disk_max_pct::float8`, cast: "float8", neutral: "0", defaultDesc: true},
	"last_seen": {expr: `h.last_seen_at`, cast: "timestamptz", neutral: "'epoch'", defaultDesc: true},
}

// HostSortDefaultDesc dice si el orden existe y si su direccion natural es descendente (los valores
// numericos y de fecha van de mayor a menor; los nombres y la severidad de menor a mayor).
func HostSortDefaultDesc(sort string) (desc, ok bool) {
	s, ok := hostSorts[sort]
	return s.defaultDesc, ok
}

// HostCursor es la posicion keyset de la ultima fila de una pagina: (NULL?, valor del orden, nombre, id).
// Es un valor interno del store; la API lo firma y lo codifica en un cursor opaco.
type HostCursor struct {
	NullFlag int
	SortKey  string
	LName    string
	ID       string
}

// HostFilter son los filtros, orden y paginacion de ListHosts. Quien llama valida formatos y limites.
type HostFilter struct {
	Q        string   // subcadena del hostname, sin distinguir mayusculas
	Statuses []string // vacio = todos
	GroupIDs []string // vacio y !NoGroup = cualquier grupo; varios se combinan con OR
	NoGroup  bool     // incluye los hosts sin grupo (OR con GroupIDs)
	OSes     []string // vacio = cualquiera; varios con OR (sin distinguir mayusculas)
	Kinds    []string // vacio = cualquiera; 'agent' | 'device' (varios con OR)

	// Filtros de metricas (nil = sin filtro). Un host sin el dato no los cumple. Quien llama valida rangos.
	CPUGt      *float64 // cpu_pct > x
	MemGt      *float64 // mem_used_mb / mem_total_mb * 100 > x
	DiskGt     *float64 // disk_max_pct > x
	BatteryLt  *float64 // battery_pct < x (sin bateria queda fuera)
	SeenBefore int      // last_seen_at < now() - N segundos; 0 = sin filtro

	Sort  string // clave de hostSorts
	Desc  bool
	Limit int
	After *HostCursor
}

// HostRow es un host de la lista con lo que necesita el dashboard.
type HostRow struct {
	ID              string
	Hostname        string
	Kind            string
	Status          string
	StatusChangedAt time.Time
	LastSeenAt      *time.Time
	Group           *GroupRef
	OS              string
	OSName          *string
	OSVersion       *string
	IPs             []netip.Addr
	CPUPct          *float64
	MemUsedMB       *int
	MemTotalMB      *int
	DiskPct         *float64
	DiskMount       *string
	BootTime        *time.Time
	BatteryPct      *int
	BatteryCharging *bool
}

// HostPage es una pagina de la lista: Total cuenta todos los hosts que cumplen los filtros (no solo la pagina).
type HostPage struct {
	Items      []HostRow
	NextCursor *HostCursor // nil = era la ultima pagina
	Total      int
}

// argList acumula parametros y devuelve el marcador $n de cada uno.
type argList []any

func (a *argList) add(v any) string {
	*a = append(*a, v)
	return fmt.Sprintf("$%d", len(*a))
}

func lowerAll(in []string) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = strings.ToLower(v)
	}
	return out
}

// ListHosts devuelve una pagina de hosts con filtros, orden y paginacion keyset. El orden total es
// (host sin valor al final, valor del orden segun direccion, nombre, id): el nombre y el id siempre
// ascendentes, asi el cursor (nombre no es unico) identifica una posicion exacta. Las etiquetas de la
// pagina se cargan en una segunda consulta (sin N+1). scope limita los hosts visibles: aplica a la lista,
// al total y al cursor por igual (se une con AND a los filtros).
func (s *Store) ListHosts(ctx context.Context, scope Scope, f HostFilter) (HostPage, error) {
	sortSpec, ok := hostSorts[f.Sort]
	if !ok {
		return HostPage{}, fmt.Errorf("orden de hosts desconocido %q", f.Sort)
	}
	limit := min(max(f.Limit, 1), MaxHostListLimit)

	var args argList
	where := []string{scope.groupCond("h.group_id", &args)}
	if f.Q != "" {
		where = append(where, fmt.Sprintf(`strpos(lower(h.hostname), lower(%s)) > 0`, args.add(f.Q)))
	}
	if len(f.Statuses) > 0 {
		where = append(where, fmt.Sprintf(`h.status = ANY(%s::text[])`, args.add(f.Statuses)))
	}
	var groupConds []string
	if len(f.GroupIDs) > 0 {
		groupConds = append(groupConds, fmt.Sprintf(`h.group_id = ANY(%s::uuid[])`, args.add(f.GroupIDs)))
	}
	if f.NoGroup {
		groupConds = append(groupConds, `h.group_id IS NULL`)
	}
	if len(groupConds) > 0 {
		where = append(where, "("+strings.Join(groupConds, " OR ")+")")
	}
	if len(f.Kinds) > 0 {
		where = append(where, fmt.Sprintf(`h.kind = ANY(%s::text[])`, args.add(f.Kinds)))
	}
	if len(f.OSes) > 0 {
		where = append(where, fmt.Sprintf(`lower(h.os) = ANY(%s::text[])`, args.add(lowerAll(f.OSes))))
	}
	if f.CPUGt != nil {
		where = append(where, fmt.Sprintf(`l.cpu_pct::float8 > %s::float8`, args.add(*f.CPUGt)))
	}
	if f.MemGt != nil {
		where = append(where, fmt.Sprintf(`l.mem_used_mb::float8 * 100 / NULLIF(l.mem_total_mb, 0) > %s::float8`, args.add(*f.MemGt)))
	}
	if f.DiskGt != nil {
		where = append(where, fmt.Sprintf(`l.disk_max_pct::float8 > %s::float8`, args.add(*f.DiskGt)))
	}
	if f.BatteryLt != nil {
		where = append(where, fmt.Sprintf(`l.battery_pct::float8 < %s::float8`, args.add(*f.BatteryLt)))
	}
	if f.SeenBefore > 0 {
		where = append(where, fmt.Sprintf(`h.last_seen_at < now() - %s::int * interval '1 second'`, args.add(f.SeenBefore)))
	}
	whereSQL := strings.Join(where, " AND ")

	dir, op := "ASC", ">"
	if f.Desc {
		dir, op = "DESC", "<"
	}
	// Condicion keyset: expansion lexicografica de (nf ASC, sk dir, ln ASC, id ASC).
	cursorSQL := "TRUE"
	if c := f.After; c != nil {
		nf, sk, ln, id := args.add(c.NullFlag), args.add(c.SortKey), args.add(c.LName), args.add(c.ID)
		skv := fmt.Sprintf("%s::%s", sk, sortSpec.cast)
		cursorSQL = fmt.Sprintf(
			`(nf > %[1]s::int) OR (nf = %[1]s::int AND sk %[5]s %[2]s) `+
				`OR (nf = %[1]s::int AND sk = %[2]s AND ln > %[3]s::text) `+
				`OR (nf = %[1]s::int AND sk = %[2]s AND ln = %[3]s::text AND id > %[4]s::uuid)`,
			nf, skv, ln, id, op)
	}

	// hostSorts y dir/op salen de listas blancas; los valores del cliente van siempre como parametros.
	query := fmt.Sprintf(`
WITH base AS (
  SELECT h.id, h.hostname, h.kind, h.status, h.status_changed_at, h.last_seen_at, g.id AS group_id, g.name AS group_name,
         COALESCE(h.os, '') AS os, hi.os_name, hi.os_version, hi.ips, hi.boot_time,
         l.cpu_pct, l.mem_used_mb, l.mem_total_mb, l.disk_max_pct, l.disk_max_mount, l.battery_pct, l.battery_charging,
         CASE WHEN (%[1]s) IS NULL THEN 1 ELSE 0 END AS nf,
         COALESCE((%[1]s)::%[2]s, %[3]s::%[2]s) AS sk,
         lower(h.hostname) AS ln,
         count(*) OVER () AS total
    FROM hosts h
    LEFT JOIN host_groups g ON g.id = h.group_id
    LEFT JOIN host_latest l ON l.host_id = h.id
    LEFT JOIN host_info hi ON hi.host_id = h.id
   WHERE %[4]s
)
SELECT id, hostname, kind, status, status_changed_at, last_seen_at, group_id, group_name, os, os_name, os_version, ips, boot_time,
       cpu_pct, mem_used_mb, mem_total_mb, disk_max_pct, disk_max_mount, battery_pct, battery_charging,
       nf, sk::text AS sk_text, ln, total
  FROM base
 WHERE %[5]s
 ORDER BY nf, sk %[6]s, ln, id
 LIMIT %[7]d`,
		sortSpec.expr, sortSpec.cast, sortSpec.neutral, whereSQL, cursorSQL, dir, limit+1)

	rows, err := s.pool.Query(ctx, query, []any(args)...)
	if err != nil {
		return HostPage{}, fmt.Errorf("listando hosts: %w", err)
	}
	defer rows.Close()

	var page HostPage
	var cursors []HostCursor
	for rows.Next() {
		var h HostRow
		var c HostCursor
		var groupID, groupName *string
		var total int
		if err := rows.Scan(&h.ID, &h.Hostname, &h.Kind, &h.Status, &h.StatusChangedAt, &h.LastSeenAt, &groupID, &groupName, &h.OS,
			&h.OSName, &h.OSVersion, &h.IPs, &h.BootTime, &h.CPUPct, &h.MemUsedMB, &h.MemTotalMB, &h.DiskPct, &h.DiskMount, &h.BatteryPct, &h.BatteryCharging,
			&c.NullFlag, &c.SortKey, &c.LName, &total); err != nil {
			return HostPage{}, fmt.Errorf("leyendo host: %w", err)
		}
		if groupID != nil && groupName != nil {
			h.Group = &GroupRef{ID: *groupID, Name: *groupName}
		}
		c.ID = h.ID
		page.Total = total
		page.Items = append(page.Items, h)
		cursors = append(cursors, c)
	}
	if err := rows.Err(); err != nil {
		return HostPage{}, fmt.Errorf("iterando hosts: %w", err)
	}
	rows.Close()

	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		next := cursors[limit-1]
		page.NextCursor = &next
	}
	if len(page.Items) == 0 {
		page.Items = []HostRow{}
		return page, nil
	}
	return page, nil
}

// HostInfo es la info estatica del host (host_info) mas la memoria total del ultimo reporte. Todo es
// opcional: un agente viejo no manda nada de esto.
type HostInfo struct {
	BootTime      *time.Time
	CPUModel      *string
	CPUCores      *int
	CPUThreads    *int
	OSName        *string
	OSVersion     *string
	KernelVersion *string
	AgentVersion  *string
	IPs           []netip.Addr
	MemTotalMB    *int
}

// HostDetail es la ficha completa de un host pa el dashboard.
type HostDetail struct {
	ID              string
	Hostname        string
	Kind            string
	OS              string
	Arch            string
	Status          string
	LastSeenAt      *time.Time
	StatusChangedAt time.Time
	CreatedAt       time.Time
	Group           *GroupRef
	Info            HostInfo
}

// HostByID devuelve la ficha del host (con grupo, etiquetas e info) o ErrNotFound. Un host fuera del
// ambito es indistinguible de uno inexistente.
func (s *Store) HostByID(ctx context.Context, scope Scope, hostID string) (HostDetail, error) {
	var h HostDetail
	var groupID, groupName *string
	args := argList{hostID}
	err := s.pool.QueryRow(ctx,
		`SELECT h.id, h.hostname, h.kind, COALESCE(h.os, ''), COALESCE(h.arch, ''), h.status, h.last_seen_at, h.status_changed_at, h.created_at,
		        g.id, g.name,
		        i.boot_time, i.cpu_model, i.cpu_cores, i.cpu_threads, i.os_name, i.os_version, i.kernel_version, i.agent_version, i.ips,
		        l.mem_total_mb
		   FROM hosts h
		   LEFT JOIN host_groups g ON g.id = h.group_id
		   LEFT JOIN host_info i ON i.host_id = h.id
		   LEFT JOIN host_latest l ON l.host_id = h.id
		  WHERE h.id = $1 AND `+scope.groupCond("h.group_id", &args), []any(args)...,
	).Scan(&h.ID, &h.Hostname, &h.Kind, &h.OS, &h.Arch, &h.Status, &h.LastSeenAt, &h.StatusChangedAt, &h.CreatedAt,
		&groupID, &groupName,
		&h.Info.BootTime, &h.Info.CPUModel, &h.Info.CPUCores, &h.Info.CPUThreads, &h.Info.OSName, &h.Info.OSVersion,
		&h.Info.KernelVersion, &h.Info.AgentVersion, &h.Info.IPs, &h.Info.MemTotalMB)
	if errors.Is(err, pgx.ErrNoRows) {
		return HostDetail{}, ErrNotFound
	}
	if err != nil {
		return HostDetail{}, fmt.Errorf("leyendo host: %w", err)
	}
	if groupID != nil && groupName != nil {
		h.Group = &GroupRef{ID: *groupID, Name: *groupName}
	}
	return h, nil
}

// DiskUsage es la ultima muestra de un montaje.
type DiskUsage struct {
	Mount   string
	UsedMB  int
	TotalMB int
	TS      time.Time
}

// diskWindow acota cuanto hacia atras se busca la ultima muestra de cada montaje (poda particiones y
// esconde montajes que ya no existen). Un equipo apagado un fin de semana largo aun muestra su ultimo dato.
const diskWindow = 7 * 24 * time.Hour

// HostDisks devuelve la ultima muestra de cada montaje del host dentro de diskWindow, por punto de montaje.
func (s *Store) HostDisks(ctx context.Context, hostID string, now time.Time) ([]DiskUsage, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT ON (mount) mount, used_mb, total_mb, ts
		   FROM disk_samples WHERE host_id = $1 AND ts >= $2 ORDER BY mount, ts DESC`,
		hostID, now.Add(-diskWindow))
	if err != nil {
		return nil, fmt.Errorf("leyendo discos: %w", err)
	}
	defer rows.Close()

	disks := []DiskUsage{}
	for rows.Next() {
		var d DiskUsage
		if err := rows.Scan(&d.Mount, &d.UsedMB, &d.TotalMB, &d.TS); err != nil {
			return nil, fmt.Errorf("leyendo disco: %w", err)
		}
		d.TS = d.TS.UTC()
		disks = append(disks, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterando discos: %w", err)
	}
	return disks, nil
}
