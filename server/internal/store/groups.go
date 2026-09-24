package store

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// GroupRef es la referencia corta a un grupo (host y listas).
type GroupRef struct {
	ID   string
	Name string
}

// Group es un grupo de hosts con su politica de disponibilidad.
type Group struct {
	ID   string
	Name string
	// DownAfterSeconds es el umbral propio del grupo; nil = usa el global (ARGOS_HOST_DOWN_AFTER).
	DownAfterSeconds *int
	// AlwaysOn: un host que deja de reportar pasa a down (true) u offline (false).
	AlwaysOn  bool
	HostCount int
}

// GroupChange es un cambio parcial de un grupo: cada campo nil se deja como esta.
// DownAfterSet distingue "no tocar" de "volver al global" (DownAfterSeconds nil).
type GroupChange struct {
	Name             *string
	DownAfterSet     bool
	DownAfterSeconds *int
	AlwaysOn         *bool
}

// Constraint del indice unico del nombre (00007) y FK de hosts.group_id (nombre autogenerado por Postgres).
const (
	groupNameKey  = "host_groups_name_lower_key"
	hostGroupFKey = "hosts_group_id_fkey"
)

// ListGroups devuelve los grupos con su cantidad de hosts (por nombre) y cuantos hosts no tienen grupo.
func (s *Store) ListGroups(ctx context.Context) (groups []Group, ungrouped int, err error) {
	rows, err := s.pool.Query(ctx,
		`SELECT g.id, g.name, g.down_after_seconds, g.always_on, count(h.id)::int
		   FROM host_groups g LEFT JOIN hosts h ON h.group_id = g.id
		  GROUP BY g.id ORDER BY lower(g.name), g.id`)
	if err != nil {
		return nil, 0, fmt.Errorf("listando grupos: %w", err)
	}
	groups, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Group, error) {
		var g Group
		err := row.Scan(&g.ID, &g.Name, &g.DownAfterSeconds, &g.AlwaysOn, &g.HostCount)
		return g, err
	})
	if err != nil {
		return nil, 0, fmt.Errorf("leyendo grupos: %w", err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*)::int FROM hosts WHERE group_id IS NULL`).Scan(&ungrouped); err != nil {
		return nil, 0, fmt.Errorf("contando hosts sin grupo: %w", err)
	}
	return groups, ungrouped, nil
}

// CreateGroup crea un grupo. ErrTooManyGroups si ya hay MaxGroups (el lock advisory hace exacto el tope
// con creaciones concurrentes) y ErrGroupExists si el nombre ya existe (sin distinguir mayusculas).
func (s *Store) CreateGroup(ctx context.Context, name string, downAfterSeconds *int, alwaysOn bool) (Group, error) {
	g := Group{Name: name, DownAfterSeconds: downAfterSeconds, AlwaysOn: alwaysOn}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('argos.host_groups.cap'))`); err != nil {
			return fmt.Errorf("lock de grupos: %w", err)
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM host_groups`).Scan(&n); err != nil {
			return fmt.Errorf("contando grupos: %w", err)
		}
		if n >= MaxGroups {
			return ErrTooManyGroups
		}
		err := tx.QueryRow(ctx,
			`INSERT INTO host_groups (name, down_after_seconds, always_on) VALUES ($1, $2, $3) RETURNING id`,
			name, downAfterSeconds, alwaysOn).Scan(&g.ID)
		if isUniqueViolation(err, groupNameKey) {
			return ErrGroupExists
		}
		if err != nil {
			return fmt.Errorf("insertando grupo: %w", err)
		}
		return nil
	})
	if err != nil {
		return Group{}, err
	}
	return g, nil
}

// UpdateGroup aplica un cambio parcial y devuelve el grupo resultante. Si cambia always_on reclasifica en
// la misma transaccion los hosts del grupo que ya estan down u offline (ver reclassifyGroupSQL). Devuelve
// ErrNotFound si no existe y ErrGroupExists si el nombre nuevo choca.
func (s *Store) UpdateGroup(ctx context.Context, id string, ch GroupChange) (Group, error) {
	var g Group
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx,
			`UPDATE host_groups SET
			     name = COALESCE($2, name),
			     down_after_seconds = CASE WHEN $3 THEN $4 ELSE down_after_seconds END,
			     always_on = COALESCE($5, always_on)
			  WHERE id = $1
			RETURNING id, name, down_after_seconds, always_on`,
			id, ch.Name, ch.DownAfterSet, ch.DownAfterSeconds, ch.AlwaysOn,
		).Scan(&g.ID, &g.Name, &g.DownAfterSeconds, &g.AlwaysOn)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if isUniqueViolation(err, groupNameKey) {
			return ErrGroupExists
		}
		if err != nil {
			return fmt.Errorf("actualizando grupo: %w", err)
		}
		if ch.AlwaysOn != nil {
			if _, err := tx.Exec(ctx, reclassifyGroupSQL, id); err != nil {
				return fmt.Errorf("reclasificando hosts del grupo: %w", err)
			}
		}
		if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM hosts WHERE group_id = $1`, id).Scan(&g.HostCount); err != nil {
			return fmt.Errorf("contando hosts del grupo: %w", err)
		}
		return nil
	})
	if err != nil {
		return Group{}, err
	}
	return g, nil
}

// DeleteGroup borra un grupo vacio. ErrGroupNotEmpty si tiene hosts (FK RESTRICT), ErrNotFound si no existe.
func (s *Store) DeleteGroup(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM host_groups WHERE id = $1`, id)
	if isFKViolation(err, hostGroupFKey) {
		return ErrGroupNotEmpty
	}
	if err != nil {
		return fmt.Errorf("borrando grupo: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Reclasificacion de hosts ya caidos: un host down en un grupo que no es always_on (o sin grupo) es en
// realidad offline, y un offline en un grupo always_on es down. Solo cambia la etiqueta: status_changed_at
// NO se mueve (el host lleva sin reportar desde entonces; no es una transicion real de disponibilidad).
// Los hosts up, pending y disabled no se tocan: los up se reevaluan solos en el siguiente ciclo del detector.
// Cada cambio real queda en host_events (source group_change) en la misma sentencia.
const (
	reclassifyGroupSQL = `
WITH upd AS (
  UPDATE hosts h SET status = t.target
    FROM (SELECT h2.id, h2.status AS old_status,
                 CASE WHEN COALESCE(g.always_on, false) THEN 'down' ELSE 'offline' END AS target
            FROM hosts h2 LEFT JOIN host_groups g ON g.id = h2.group_id
           WHERE h2.group_id = $1) t
   WHERE h.id = t.id AND h.status = t.old_status AND h.status IN ('down', 'offline') AND h.status <> t.target
  RETURNING h.id, t.old_status, h.status
)
INSERT INTO host_events (host_id, from_status, to_status, source)
SELECT id, old_status, status, 'group_change' FROM upd`

	reclassifyHostsSQL = `
WITH upd AS (
  UPDATE hosts h SET status = t.target
    FROM (SELECT h2.id, h2.status AS old_status,
                 CASE WHEN COALESCE(g.always_on, false) THEN 'down' ELSE 'offline' END AS target
            FROM hosts h2 LEFT JOIN host_groups g ON g.id = h2.group_id
           WHERE h2.id = ANY($1)) t
   WHERE h.id = t.id AND h.status = t.old_status AND h.status IN ('down', 'offline') AND h.status <> t.target
  RETURNING h.id, t.old_status, h.status
)
INSERT INTO host_events (host_id, from_status, to_status, source)
SELECT id, old_status, status, 'group_change' FROM upd`
)

// HostGroupResult es el estado del host tras asignarle un grupo.
type HostGroupResult struct {
	Group  *GroupRef
	Status string
}

// SetHostGroup asigna el grupo del host (nil = sin grupo) y reclasifica su estado si estaba down u
// offline. ErrNotFound si el host no existe; ErrGroupNotFound si el grupo no existe.
func (s *Store) SetHostGroup(ctx context.Context, hostID string, groupID *string) (HostGroupResult, error) {
	var res HostGroupResult
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if groupID != nil {
			// FOR SHARE: el grupo no se puede borrar mientras dura la transaccion.
			var one int
			err := tx.QueryRow(ctx, `SELECT 1 FROM host_groups WHERE id = $1 FOR SHARE`, *groupID).Scan(&one)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrGroupNotFound
			}
			if err != nil {
				return fmt.Errorf("comprobando grupo: %w", err)
			}
		}
		tag, err := tx.Exec(ctx, `UPDATE hosts SET group_id = $2 WHERE id = $1`, hostID, groupID)
		if err != nil {
			return fmt.Errorf("asignando grupo: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if _, err := tx.Exec(ctx, reclassifyHostsSQL, []string{hostID}); err != nil {
			return fmt.Errorf("reclasificando host: %w", err)
		}
		var ref GroupRef
		var refID, refName *string
		err = tx.QueryRow(ctx,
			`SELECT h.status, g.id, g.name FROM hosts h LEFT JOIN host_groups g ON g.id = h.group_id WHERE h.id = $1`,
			hostID).Scan(&res.Status, &refID, &refName)
		if err != nil {
			return fmt.Errorf("leyendo host: %w", err)
		}
		if refID != nil && refName != nil {
			ref = GroupRef{ID: *refID, Name: *refName}
			res.Group = &ref
		}
		return nil
	})
	if err != nil {
		return HostGroupResult{}, err
	}
	return res, nil
}

// SetHostTags reemplaza TODAS las etiquetas del host en una transaccion y devuelve las guardadas (orden
// alfabetico sin distinguir mayusculas). Quien llama valida formato y quita repetidos (sin distinguir
// mayusculas); aqui se serializa por host (lock de la fila del host) pa que dos reemplazos concurrentes no
// choquen, cada etiqueta se canonicaliza contra la grafia ya existente en cualquier host y el trigger de la
// BD es la ultima defensa del limite. ErrNotFound si el host no existe; ErrTooManyTags si supera MaxTagsPerHost.
func (s *Store) SetHostTags(ctx context.Context, hostID string, tags []string) ([]string, error) {
	var out []string
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var one int
		err := tx.QueryRow(ctx, `SELECT 1 FROM hosts WHERE id = $1 FOR NO KEY UPDATE`, hostID).Scan(&one)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("bloqueando host: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM host_tags WHERE host_id = $1`, hostID); err != nil {
			return fmt.Errorf("borrando etiquetas: %w", err)
		}
		if len(tags) == 0 {
			out = []string{}
			return nil
		}
		canon, err := canonicalTags(ctx, tx, tags)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO host_tags (host_id, tag) SELECT $1, t FROM unnest($2::text[]) AS t`, hostID, canon); err != nil {
			return mapTagsErr(err, "insertando etiquetas")
		}
		out = canon
		return nil
	})
	if err != nil {
		return nil, err
	}
	sortTags(out)
	return out, nil
}

// sortTags ordena alfabeticamente sin distinguir mayusculas (desempata por grafia).
func sortTags(tags []string) {
	slices.SortFunc(tags, func(a, b string) int {
		if c := cmp.Compare(strings.ToLower(a), strings.ToLower(b)); c != 0 {
			return c
		}
		return cmp.Compare(a, b)
	})
}

// canonicalTags devuelve cada etiqueta con la grafia que ya existe (sin distinguir mayusculas) en cualquier
// host, o tal cual si es nueva. Toma un lock transaccional por etiqueta (en orden) pa que dos transacciones
// que crean "Ventas" y "ventas" a la vez no fijen dos grafias distintas.
func canonicalTags(ctx context.Context, tx pgx.Tx, tags []string) ([]string, error) {
	lowers := make([]string, len(tags))
	for i, t := range tags {
		lowers[i] = strings.ToLower(t)
	}
	order := slices.Clone(lowers)
	slices.Sort(order)
	order = slices.Compact(order)
	for _, l := range order {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('argos.tag:' || $1, 0))`, l); err != nil {
			return nil, fmt.Errorf("bloqueando etiqueta: %w", err)
		}
	}
	rows, err := tx.Query(ctx,
		`SELECT DISTINCT ON (lower(tag)) lower(tag), tag FROM host_tags WHERE lower(tag) = ANY($1) ORDER BY lower(tag), tag`, lowers)
	if err != nil {
		return nil, fmt.Errorf("buscando grafia de etiquetas: %w", err)
	}
	existing := make(map[string]string, len(tags))
	for rows.Next() {
		var l, t string
		if err := rows.Scan(&l, &t); err != nil {
			rows.Close()
			return nil, fmt.Errorf("leyendo etiqueta: %w", err)
		}
		existing[l] = t
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterando etiquetas: %w", err)
	}
	out := make([]string, len(tags))
	for i, t := range tags {
		if c, ok := existing[lowers[i]]; ok {
			out[i] = c
		} else {
			out[i] = t
		}
	}
	return out, nil
}

// mapTagsErr traduce el limite de etiquetas del trigger (check_violation host_tags_max_per_host).
func mapTagsErr(err error, what string) error {
	if isPgConstraint(err, sqlstateCheckViolation, "host_tags_max_per_host") {
		return ErrTooManyTags
	}
	return fmt.Errorf("%s: %w", what, err)
}

// TagCount es una etiqueta en uso con la cantidad de hosts que la tienen.
type TagCount struct {
	Name  string
	Count int
}

// maxTagsListed acota ListTags: la respuesta es pa sugerir y filtrar, no un inventario.
const maxTagsListed = 500

// ListTags devuelve las etiquetas en uso (agrupadas sin distinguir mayusculas) con su conteo de hosts, por
// conteo descendente y luego nombre, hasta maxTagsListed.
func (s *Store) ListTags(ctx context.Context) ([]TagCount, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT min(tag), count(*)::int FROM host_tags GROUP BY lower(tag) ORDER BY count(*) DESC, lower(min(tag)), min(tag) LIMIT $1`,
		maxTagsListed)
	if err != nil {
		return nil, fmt.Errorf("listando etiquetas: %w", err)
	}
	defer rows.Close()

	tags := []TagCount{}
	for rows.Next() {
		var t TagCount
		if err := rows.Scan(&t.Name, &t.Count); err != nil {
			return nil, fmt.Errorf("leyendo etiqueta: %w", err)
		}
		tags = append(tags, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterando etiquetas: %w", err)
	}
	return tags, nil
}

// Acciones de BulkHosts.
const (
	BulkSetGroup  = "set_group"
	BulkAddTag    = "add_tag"
	BulkRemoveTag = "remove_tag"
	BulkDisable   = "disable"
)

// Bulk describe una accion masiva sobre hosts (ids ya validados y sin repetir).
type Bulk struct {
	IDs    []string
	Action string
	// GroupID (set_group): nil = dejar sin grupo.
	GroupID *string
	// Tag (add_tag / remove_tag; remove_tag no distingue mayusculas).
	Tag string
}

// BulkHosts aplica la accion a todos los hosts en una transaccion (todo o nada) y devuelve cuantos hosts
// cambiaron de verdad. ErrUnknownHosts si alguno no existe; ErrGroupNotFound (set_group); ErrTooManyTags
// (add_tag) si algun host superaria el maximo.
func (s *Store) BulkHosts(ctx context.Context, b Bulk) (int, error) {
	var affected int
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Se bloquean en orden de id (sin interbloqueo entre dos bulk cruzados) y se comprueba que existan todos.
		rows, err := tx.Query(ctx, `SELECT id FROM hosts WHERE id = ANY($1) ORDER BY id FOR NO KEY UPDATE`, b.IDs)
		if err != nil {
			return fmt.Errorf("bloqueando hosts: %w", err)
		}
		found, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return fmt.Errorf("leyendo hosts: %w", err)
		}
		if len(found) != len(b.IDs) {
			return ErrUnknownHosts
		}

		var tag pgconn.CommandTag
		switch b.Action {
		case BulkSetGroup:
			if b.GroupID != nil {
				var one int
				err := tx.QueryRow(ctx, `SELECT 1 FROM host_groups WHERE id = $1 FOR SHARE`, *b.GroupID).Scan(&one)
				if errors.Is(err, pgx.ErrNoRows) {
					return ErrGroupNotFound
				}
				if err != nil {
					return fmt.Errorf("comprobando grupo: %w", err)
				}
			}
			tag, err = tx.Exec(ctx, `UPDATE hosts SET group_id = $2 WHERE id = ANY($1) AND group_id IS DISTINCT FROM $2`, b.IDs, b.GroupID)
			if err != nil {
				return fmt.Errorf("asignando grupo: %w", err)
			}
			if _, err := tx.Exec(ctx, reclassifyHostsSQL, b.IDs); err != nil {
				return fmt.Errorf("reclasificando hosts: %w", err)
			}
		case BulkAddTag:
			canon, err := canonicalTags(ctx, tx, []string{b.Tag})
			if err != nil {
				return err
			}
			tag, err = tx.Exec(ctx,
				`INSERT INTO host_tags (host_id, tag) SELECT u, $2 FROM unnest($1::uuid[]) AS u
				 ON CONFLICT (host_id, lower(tag)) DO NOTHING`,
				b.IDs, canon[0])
			if err != nil {
				return mapTagsErr(err, "agregando etiqueta")
			}
		case BulkRemoveTag:
			tag, err = tx.Exec(ctx,
				`DELETE FROM host_tags WHERE host_id = ANY($1) AND lower(tag) = lower($2)`, b.IDs, b.Tag)
			if err != nil {
				return fmt.Errorf("quitando etiqueta: %w", err)
			}
		case BulkDisable:
			if err := tx.QueryRow(ctx, disableHostsSQL, b.IDs).Scan(&affected); err != nil {
				return fmt.Errorf("revocando hosts: %w", err)
			}
			return nil
		default:
			return fmt.Errorf("accion masiva desconocida %q", b.Action)
		}
		affected = int(tag.RowsAffected())
		return nil
	})
	if err != nil {
		return 0, err
	}
	return affected, nil
}
