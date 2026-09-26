package store

import (
	"context"
	"errors"
	"fmt"

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
	// DetailRetentionDays es la retencion de detalle propia del grupo (nil = usa la global); EffectiveDetailDays y
	// RetentionSource (group o global) son el valor vigente y su origen.
	DetailRetentionDays *int
	EffectiveDetailDays int
	RetentionSource     string
}

// GroupChange es un cambio parcial de un grupo: cada campo nil se deja como esta.
// DownAfterSet distingue "no tocar" de "volver al global" (DownAfterSeconds nil).
type GroupChange struct {
	Name             *string
	DownAfterSet     bool
	DownAfterSeconds *int
	AlwaysOn         *bool
	// DetailSet distingue "no tocar" de "heredar la global" (DetailDays nil).
	DetailSet  bool
	DetailDays *int
}

// Constraint del indice unico del nombre (00007) y FK de hosts.group_id (nombre autogenerado por Postgres).
const (
	groupNameKey  = "host_groups_name_lower_key"
	hostGroupFKey = "hosts_group_id_fkey"
)

// ListGroups devuelve los grupos con su cantidad de hosts (por nombre) y cuantos hosts no tienen grupo.
// Con un ambito restringido solo salen sus grupos y ungrouped es 0 (nunca ve hosts sin grupo).
func (s *Store) ListGroups(ctx context.Context, scope Scope) (groups []Group, ungrouped int, err error) {
	var args argList
	// Un grupo esta en el ambito si sus hosts lo estan: la misma condicion, sobre g.id.
	rows, err := s.pool.Query(ctx,
		`SELECT g.id, g.name, g.down_after_seconds, g.always_on, g.detail_retention_days, count(h.id)::int
		   FROM host_groups g LEFT JOIN hosts h ON h.group_id = g.id
		  WHERE `+scope.groupCond("g.id", &args)+`
		  GROUP BY g.id ORDER BY lower(g.name), g.id`, []any(args)...)
	if err != nil {
		return nil, 0, fmt.Errorf("listando grupos: %w", err)
	}
	groups, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Group, error) {
		var g Group
		err := row.Scan(&g.ID, &g.Name, &g.DownAfterSeconds, &g.AlwaysOn, &g.DetailRetentionDays, &g.HostCount)
		return g, err
	})
	if err != nil {
		return nil, 0, fmt.Errorf("leyendo grupos: %w", err)
	}
	if err := s.fillGroupRetention(ctx, groups); err != nil {
		return nil, 0, err
	}
	if !scope.All() {
		return groups, 0, nil
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
	return s.withRetention(ctx, g)
}

// fillGroupRetention completa el valor efectivo de la retencion de detalle de cada grupo.
func (s *Store) fillGroupRetention(ctx context.Context, groups []Group) error {
	if len(groups) == 0 {
		return nil
	}
	global, _, err := s.globalDetail(ctx)
	if err != nil {
		return err
	}
	for i := range groups {
		groups[i].EffectiveDetailDays, groups[i].RetentionSource = EffectiveDetail(nil, groups[i].DetailRetentionDays, global)
	}
	return nil
}

func (s *Store) withRetention(ctx context.Context, g Group) (Group, error) {
	gs := []Group{g}
	if err := s.fillGroupRetention(ctx, gs); err != nil {
		return Group{}, err
	}
	return gs[0], nil
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
			     always_on = COALESCE($5, always_on),
			     detail_retention_days = CASE WHEN $6 THEN $7 ELSE detail_retention_days END
			  WHERE id = $1
			RETURNING id, name, down_after_seconds, always_on, detail_retention_days`,
			id, ch.Name, ch.DownAfterSet, ch.DownAfterSeconds, ch.AlwaysOn, ch.DetailSet, ch.DetailDays,
		).Scan(&g.ID, &g.Name, &g.DownAfterSeconds, &g.AlwaysOn, &g.DetailRetentionDays)
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
	return s.withRetention(ctx, g)
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

// Acciones de BulkHosts.
const (
	BulkSetGroup = "set_group"
	BulkDisable  = "disable"
)

// Bulk describe una accion masiva sobre hosts (ids ya validados y sin repetir).
type Bulk struct {
	IDs    []string
	Action string
	// GroupID (set_group): nil = dejar sin grupo.
	GroupID *string
}

// BulkHosts aplica la accion a todos los hosts en una transaccion (todo o nada) y devuelve cuantos hosts
// cambiaron de verdad. ErrUnknownHosts si alguno no existe; ErrGroupNotFound (set_group).
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

// RenameHost cambia el nombre mostrado de un host con agente. ErrNotFound si no existe o es un dispositivo (esos se
// renombran con UpdateDevice).
func (s *Store) RenameHost(ctx context.Context, hostID, name string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE hosts SET hostname = $2 WHERE id = $1 AND kind = 'agent'`, hostID, name)
	if err != nil {
		return fmt.Errorf("renombrando host: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
