package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Limites de la lista de flota de parches (mismos que la de hosts).
const (
	DefaultUpdatesFleetLimit = DefaultHostListLimit
	MaxUpdatesFleetLimit     = MaxHostListLimit
)

// updatesFleetSorts es la lista blanca de ordenes de la flota (los fragmentos SQL salen de aqui, nunca del
// cliente). Los hosts sin valor (dato desconocido) van siempre al final. security es el orden por defecto: los
// hosts con mas pendientes de seguridad primero.
var updatesFleetSorts = map[string]hostSort{
	"security": {expr: `u.pending_security`, cast: "int", neutral: "0", defaultDesc: true},
	"pending":  {expr: `u.pending_total`, cast: "int", neutral: "0", defaultDesc: true},
	// reboot: el reinicio pendiente mas antiguo primero.
	"reboot":  {expr: `u.reboot_since`, cast: "timestamptz", neutral: "'epoch'"},
	"eol":     {expr: `eo.eol_date`, cast: "date", neutral: "'epoch'"},
	"name":    {expr: `lower(h.hostname)`, cast: "text", neutral: "''"},
	"updated": {expr: `u.received_at`, cast: "timestamptz", neutral: "'epoch'", defaultDesc: true},
}

// UpdatesFleetSortDefaultDesc dice si el orden existe y si su direccion natural es descendente.
func UpdatesFleetSortDefaultDesc(sort string) (desc, ok bool) {
	s, ok := updatesFleetSorts[sort]
	return s.defaultDesc, ok
}

// UpdatesFleetFilter son los filtros, orden y paginacion de ListUpdatesFleet. Quien llama valida formatos y
// limites.
type UpdatesFleetFilter struct {
	Q        string   // subcadena del hostname, sin distinguir mayusculas
	GroupIDs []string // vacio y !NoGroup = cualquier grupo; varios con OR
	NoGroup  bool     // incluye los hosts sin grupo (OR con GroupIDs)
	OSes     []string // vacio = cualquiera (windows, linux, darwin)

	SecurityOnly  bool // solo con pendientes de seguridad (> 0)
	RebootOnly    bool // solo con reinicio pendiente
	EOLWithinDays *int // solo con fin de vida en <= N dias (o ya vencido); sin dato de EOL queda fuera

	Sort  string // clave de updatesFleetSorts
	Desc  bool
	Limit int
	After *HostCursor
}

// UpdatesFleetRow es un host de la flota con su foto de parches y su fin de vida (nil = desconocido).
type UpdatesFleetRow struct {
	HostID    string
	Hostname  string
	Status    string
	Group     *GroupRef
	OS        string
	OSName    *string
	OSVersion *string
	Updates   UpdatesCurrent
	EOL       *OSEOL
}

// UpdatesFleetPage es una pagina de la flota; Total cuenta todos los hosts que cumplen los filtros.
type UpdatesFleetPage struct {
	Items      []UpdatesFleetRow
	NextCursor *HostCursor // nil = era la ultima pagina
	Total      int
}

// ListUpdatesFleet devuelve una pagina de hosts con foto de parches (los que nunca reportaron el modulo y los
// deshabilitados no salen), con el ambito, filtros, orden y paginacion keyset, igual que ListHosts: el orden
// total es (sin valor al final, valor, nombre, id) y una sola consulta trae la foto y el fin de vida (sin N+1).
// updatesFleetFilterWhere arma el WHERE de UpdatesFleetFilter (sin orden ni paginacion): lo comparten
// ListUpdatesFleet y ExportUpdatesFleet.
func updatesFleetFilterWhere(scope Scope, f UpdatesFleetFilter) (string, argList) {
	var args argList
	where := []string{`h.status <> 'disabled'`, scope.groupCond("h.group_id", &args)}
	if f.Q != "" {
		where = append(where, fmt.Sprintf(`strpos(lower(h.hostname), lower(%s)) > 0`, args.add(f.Q)))
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
	if len(f.OSes) > 0 {
		where = append(where, fmt.Sprintf(`lower(h.os) = ANY(%s::text[])`, args.add(lowerAll(f.OSes))))
	}
	if f.SecurityOnly {
		where = append(where, `u.pending_security > 0`)
	}
	if f.RebootOnly {
		where = append(where, `u.reboot_required IS TRUE`)
	}
	if f.EOLWithinDays != nil {
		where = append(where, fmt.Sprintf(`eo.eol_date - (now() AT TIME ZONE 'UTC')::date <= %s::int`, args.add(*f.EOLWithinDays)))
	}
	return strings.Join(where, " AND "), args
}

func (s *Store) ListUpdatesFleet(ctx context.Context, scope Scope, f UpdatesFleetFilter) (UpdatesFleetPage, error) {
	sortSpec, ok := updatesFleetSorts[f.Sort]
	if !ok {
		return UpdatesFleetPage{}, fmt.Errorf("orden de parches desconocido %q", f.Sort)
	}
	limit := min(max(f.Limit, 1), MaxUpdatesFleetLimit)

	whereSQL, args := updatesFleetFilterWhere(scope, f)

	dir, op := "ASC", ">"
	if f.Desc {
		dir, op = "DESC", "<"
	}
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

	// updatesFleetSorts y dir/op salen de listas blancas; los valores del cliente van siempre como parametros.
	query := fmt.Sprintf(`
WITH base AS (
  SELECT h.id, h.hostname, h.status, g.id AS group_id, g.name AS group_name, COALESCE(h.os, '') AS os,
         hi.os_name, hi.os_version,
         u.rev, u.pending_total, u.pending_security, u.reboot_required, u.reboot_since, u.security_pending_since,
         u.last_installed, u.source, u.cache_age_s, u.advisories, u.advisories_truncated, u.collected_at, u.received_at,
         eo.family, eo.cycle, eo.label, eo.eol_date, eo.lts, eo.eol_source,
         CASE WHEN (%[1]s) IS NULL THEN 1 ELSE 0 END AS nf,
         COALESCE((%[1]s)::%[2]s, %[3]s::%[2]s) AS sk,
         lower(h.hostname) AS ln,
         count(*) OVER () AS total
    FROM updates_current u
    JOIN hosts h ON h.id = u.host_id
    LEFT JOIN host_groups g ON g.id = h.group_id
    LEFT JOIN host_info hi ON hi.host_id = h.id
    LEFT JOIN LATERAL (
      SELECT e.family, e.cycle, e.label, e.eol_date, e.lts, e.source AS eol_source
        FROM os_eol_key(h.os, hi.os_name, hi.os_version) k
        JOIN os_eol e ON e.family = k.family AND e.cycle = k.cycle) eo ON true
   WHERE %[4]s
)
SELECT id, hostname, status, group_id, group_name, os, os_name, os_version,
       rev, pending_total, pending_security, reboot_required, reboot_since, security_pending_since,
       last_installed, source, cache_age_s, advisories, advisories_truncated, collected_at, received_at,
       family, cycle, label, eol_date, lts, eol_source,
       nf, sk::text AS sk_text, ln, total
  FROM base
 WHERE %[5]s
 ORDER BY nf, sk %[6]s, ln, id
 LIMIT %[7]d`,
		sortSpec.expr, sortSpec.cast, sortSpec.neutral, whereSQL, cursorSQL, dir, limit+1)

	rows, err := s.pool.Query(ctx, query, []any(args)...)
	if err != nil {
		return UpdatesFleetPage{}, fmt.Errorf("listando flota de parches: %w", err)
	}
	defer rows.Close()

	var page UpdatesFleetPage
	var cursors []HostCursor
	for rows.Next() {
		var r UpdatesFleetRow
		var c HostCursor
		var groupID, groupName, family, cycle, label, eolSource *string
		var eolDate *time.Time
		var lts *bool
		var total int
		if err := rows.Scan(&r.HostID, &r.Hostname, &r.Status, &groupID, &groupName, &r.OS, &r.OSName, &r.OSVersion,
			&r.Updates.Rev, &r.Updates.PendingTotal, &r.Updates.PendingSecurity, &r.Updates.RebootRequired, &r.Updates.RebootSince,
			&r.Updates.SecurityPendingSince, &r.Updates.LastInstalled, &r.Updates.Source, &r.Updates.CacheAgeS, &r.Updates.Advisories,
			&r.Updates.AdvisoriesTruncated, &r.Updates.CollectedAt, &r.Updates.ReceivedAt,
			&family, &cycle, &label, &eolDate, &lts, &eolSource,
			&c.NullFlag, &c.SortKey, &c.LName, &total); err != nil {
			return UpdatesFleetPage{}, fmt.Errorf("leyendo host de la flota de parches: %w", err)
		}
		if groupID != nil && groupName != nil {
			r.Group = &GroupRef{ID: *groupID, Name: *groupName}
		}
		if family != nil && cycle != nil && label != nil && eolDate != nil && lts != nil && eolSource != nil {
			r.EOL = &OSEOL{Family: *family, Cycle: *cycle, Label: *label, EOLDate: *eolDate, LTS: *lts, Estimated: *eolSource == "estimate"}
		}
		c.ID = r.HostID
		page.Total = total
		page.Items = append(page.Items, r)
		cursors = append(cursors, c)
	}
	if err := rows.Err(); err != nil {
		return UpdatesFleetPage{}, fmt.Errorf("iterando flota de parches: %w", err)
	}
	rows.Close()

	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		next := cursors[limit-1]
		page.NextCursor = &next
	}
	if len(page.Items) == 0 {
		page.Items = []UpdatesFleetRow{}
	}
	return page, nil
}
