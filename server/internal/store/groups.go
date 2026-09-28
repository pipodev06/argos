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
	// Cuotas del cliente (B7; "cliente" = grupo, 1 a 1). nil = sin limite propio: el tope global de la
	// plataforma para ese recurso (si existe) sigue rigiendo igual; esto es un tope ADICIONAL por cliente.
	MaxHosts, MaxDevices, MaxChecks, MaxUsers, MaxRules, MaxChannels *int
}

// GroupQuota es el consumo de un recurso de un grupo frente a su cuota (nil = sin limite propio).
type GroupQuota struct {
	Current int
	Limit   *int
}

// GroupUsage es el consumo actual de los 6 recursos con cuota de un grupo (B7, "consumo visible").
type GroupUsage struct {
	Hosts, Devices, Checks, Users, Rules, Channels GroupQuota
}

// quotaResource identifica el recurso al pedir/chequear una cuota.
type quotaResource string

const (
	QuotaHosts    quotaResource = "hosts"
	QuotaDevices  quotaResource = "devices"
	QuotaChecks   quotaResource = "checks"
	QuotaUsers    quotaResource = "users"
	QuotaRules    quotaResource = "rules"
	QuotaChannels quotaResource = "channels"
)

// ErrGroupQuota: el grupo ya esta en su cuota de un recurso (B7); GroupQuotaError trae el detalle.
var ErrGroupQuota = errors.New("cuota del grupo agotada")

// GroupQuotaError es el detalle de ErrGroupQuota: que recurso, cuanto hay y cuanto permite el grupo.
type GroupQuotaError struct {
	Resource       quotaResource
	Current, Limit int
}

func (e *GroupQuotaError) Error() string {
	return fmt.Sprintf("%v: %s (%d/%d)", ErrGroupQuota, e.Resource, e.Current, e.Limit)
}

// Unwrap devuelve ErrGroupQuota.
func (e *GroupQuotaError) Unwrap() error { return ErrGroupQuota }

// countSQL da la consulta de conteo actual de cada recurso, parametrizada por el id del grupo ($1).
var quotaCountSQL = map[quotaResource]string{
	QuotaHosts:    `SELECT count(*)::int FROM hosts WHERE group_id = $1 AND kind = 'agent'`,
	QuotaDevices:  `SELECT count(*)::int FROM hosts WHERE group_id = $1 AND kind = 'device'`,
	QuotaChecks:   `SELECT count(*)::int FROM service_check_defs WHERE scope_type = 'group' AND scope_group_id = $1`,
	QuotaUsers:    `SELECT count(DISTINCT ug.user_id)::int FROM user_groups ug WHERE ug.group_id = $1`,
	QuotaRules:    `SELECT count(*)::int FROM alert_rules WHERE scope_type = 'group' AND scope_group_id = $1`,
	QuotaChannels: `SELECT count(*)::int FROM notification_channels WHERE group_id = $1`,
}

// quotaLimit devuelve el limite (columna max_*) configurado de un recurso.
func (g Group) quotaLimit(r quotaResource) *int {
	switch r {
	case QuotaHosts:
		return g.MaxHosts
	case QuotaDevices:
		return g.MaxDevices
	case QuotaChecks:
		return g.MaxChecks
	case QuotaUsers:
		return g.MaxUsers
	case QuotaRules:
		return g.MaxRules
	default:
		return g.MaxChannels
	}
}

// checkGroupQuota falla con *GroupQuotaError si el grupo ya tiene (o al sumar `extra` tendria) mas
// filas de `resource` que su cuota. `extra` es cuantas filas nuevas se estan por agregar (1 casi siempre;
// mas en un alta masiva). Sin cuota propia (nil) no hace ninguna consulta. Debe llamarse DENTRO de la misma
// transaccion que hace el alta, para que el conteo sea consistente con el resto de la operacion; `q` puede
// ser el pool o una tx (ambos implementan QueryRow).
func checkGroupQuota(ctx context.Context, q rowQuerier, groupID string, resource quotaResource, extra int) error {
	var limit *int
	if err := q.QueryRow(ctx, `SELECT `+quotaColumn(resource)+` FROM host_groups WHERE id = $1`, groupID).Scan(&limit); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // el grupo no existe: quien llama ya deberia haberlo validado antes; no es cosa de cuotas
		}
		return fmt.Errorf("leyendo cuota de %s: %w", resource, err)
	}
	if limit == nil {
		return nil
	}
	var current int
	if err := q.QueryRow(ctx, quotaCountSQL[resource], groupID).Scan(&current); err != nil {
		return fmt.Errorf("contando %s del grupo: %w", resource, err)
	}
	if current+extra > *limit {
		return &GroupQuotaError{Resource: resource, Current: current, Limit: *limit}
	}
	return nil
}

func quotaColumn(r quotaResource) string {
	switch r {
	case QuotaHosts:
		return "max_hosts"
	case QuotaDevices:
		return "max_devices"
	case QuotaChecks:
		return "max_checks"
	case QuotaUsers:
		return "max_users"
	case QuotaRules:
		return "max_rules"
	default:
		return "max_channels"
	}
}

// GroupUsageByID devuelve el consumo actual de los 6 recursos con cuota de un grupo. ErrNotFound si no existe.
func (s *Store) GroupUsageByID(ctx context.Context, groupID string) (GroupUsage, error) {
	var g Group
	err := s.pool.QueryRow(ctx,
		`SELECT max_hosts, max_devices, max_checks, max_users, max_rules, max_channels FROM host_groups WHERE id = $1`,
		groupID,
	).Scan(&g.MaxHosts, &g.MaxDevices, &g.MaxChecks, &g.MaxUsers, &g.MaxRules, &g.MaxChannels)
	if errors.Is(err, pgx.ErrNoRows) {
		return GroupUsage{}, ErrNotFound
	}
	if err != nil {
		return GroupUsage{}, fmt.Errorf("leyendo cuotas del grupo: %w", err)
	}
	var u GroupUsage
	for res, dst := range map[quotaResource]*GroupQuota{
		QuotaHosts: &u.Hosts, QuotaDevices: &u.Devices, QuotaChecks: &u.Checks,
		QuotaUsers: &u.Users, QuotaRules: &u.Rules, QuotaChannels: &u.Channels,
	} {
		if err := s.pool.QueryRow(ctx, quotaCountSQL[res], groupID).Scan(&dst.Current); err != nil {
			return GroupUsage{}, fmt.Errorf("contando %s: %w", res, err)
		}
		dst.Limit = g.quotaLimit(res)
	}
	return u, nil
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
	// Cuotas (B7): cada Set distingue "no tocar" de "quitar el limite" (valor nil). Sin Set, la columna no se toca.
	MaxHostsSet, MaxDevicesSet, MaxChecksSet, MaxUsersSet, MaxRulesSet, MaxChannelsSet bool
	MaxHosts, MaxDevices, MaxChecks, MaxUsers, MaxRules, MaxChannels                   *int
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
		`SELECT g.id, g.name, g.down_after_seconds, g.always_on, g.detail_retention_days, count(h.id)::int,
		        g.max_hosts, g.max_devices, g.max_checks, g.max_users, g.max_rules, g.max_channels
		   FROM host_groups g LEFT JOIN hosts h ON h.group_id = g.id
		  WHERE `+scope.groupCond("g.id", &args)+`
		  GROUP BY g.id ORDER BY lower(g.name), g.id`, []any(args)...)
	if err != nil {
		return nil, 0, fmt.Errorf("listando grupos: %w", err)
	}
	groups, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Group, error) {
		var g Group
		err := row.Scan(&g.ID, &g.Name, &g.DownAfterSeconds, &g.AlwaysOn, &g.DetailRetentionDays, &g.HostCount,
			&g.MaxHosts, &g.MaxDevices, &g.MaxChecks, &g.MaxUsers, &g.MaxRules, &g.MaxChannels)
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
func (s *Store) CreateGroup(ctx context.Context, name string, downAfterSeconds *int, alwaysOn bool, detailDays *int) (Group, error) {
	g := Group{Name: name, DownAfterSeconds: downAfterSeconds, AlwaysOn: alwaysOn, DetailRetentionDays: detailDays}
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
			`INSERT INTO host_groups (name, down_after_seconds, always_on, detail_retention_days) VALUES ($1, $2, $3, $4) RETURNING id`,
			name, downAfterSeconds, alwaysOn, detailDays).Scan(&g.ID)
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
			     detail_retention_days = CASE WHEN $6 THEN $7 ELSE detail_retention_days END,
			     max_hosts    = CASE WHEN $8  THEN $9  ELSE max_hosts    END,
			     max_devices  = CASE WHEN $10 THEN $11 ELSE max_devices  END,
			     max_checks   = CASE WHEN $12 THEN $13 ELSE max_checks   END,
			     max_users    = CASE WHEN $14 THEN $15 ELSE max_users    END,
			     max_rules    = CASE WHEN $16 THEN $17 ELSE max_rules    END,
			     max_channels = CASE WHEN $18 THEN $19 ELSE max_channels END
			  WHERE id = $1
			RETURNING id, name, down_after_seconds, always_on, detail_retention_days,
			          max_hosts, max_devices, max_checks, max_users, max_rules, max_channels`,
			id, ch.Name, ch.DownAfterSet, ch.DownAfterSeconds, ch.AlwaysOn, ch.DetailSet, ch.DetailDays,
			ch.MaxHostsSet, ch.MaxHosts, ch.MaxDevicesSet, ch.MaxDevices, ch.MaxChecksSet, ch.MaxChecks,
			ch.MaxUsersSet, ch.MaxUsers, ch.MaxRulesSet, ch.MaxRules, ch.MaxChannelsSet, ch.MaxChannels,
		).Scan(&g.ID, &g.Name, &g.DownAfterSeconds, &g.AlwaysOn, &g.DetailRetentionDays,
			&g.MaxHosts, &g.MaxDevices, &g.MaxChecks, &g.MaxUsers, &g.MaxRules, &g.MaxChannels)
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
		if err := checkGroupMove(ctx, tx, []string{hostID}, groupID); err != nil {
			return err
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

// checkGroupMove comprueba, antes de mover los hosts ids al grupo newGroup (nil = sin grupo), que ningun dispositivo
// quede en un grupo distinto del de su colector: un dispositivo movido sin su colector (ErrCollectorGroupMismatch) ni
// un colector movido sin sus dispositivos (ErrCollectorHasDevices), ambos con la cuenta. Moverlos a la vez es valido.
// Solo cuenta lo que cambia de verdad: un desfase anterior que la operacion no toca no la bloquea. Bloquea los hosts
// implicados y los colectores de los dispositivos movidos para que un CreateDevice o UpdateDevice concurrente vea el grupo
// nuevo (collectorGroup toma FOR SHARE).
func checkGroupMove(ctx context.Context, tx pgx.Tx, ids []string, newGroup *string) error {
	if _, err := tx.Exec(ctx, `SELECT 1 FROM hosts WHERE id = ANY($1) ORDER BY id FOR NO KEY UPDATE`, ids); err != nil {
		return fmt.Errorf("bloqueando hosts: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`SELECT 1 FROM hosts WHERE id IN (SELECT collector_host_id FROM hosts WHERE id = ANY($1) AND kind = 'device')
		   AND NOT (id = ANY($1)) ORDER BY id FOR SHARE`, ids); err != nil {
		return fmt.Errorf("bloqueando colectores: %w", err)
	}
	var away, orphaned int
	err := tx.QueryRow(ctx,
		`SELECT
		   (SELECT count(*)::int FROM hosts d JOIN hosts c ON c.id = d.collector_host_id
		     WHERE d.id = ANY($1) AND d.kind = 'device' AND NOT (c.id = ANY($1))
		       AND d.group_id IS DISTINCT FROM $2::uuid AND c.group_id IS DISTINCT FROM $2::uuid),
		   (SELECT count(*)::int FROM hosts d JOIN hosts c ON c.id = d.collector_host_id
		     WHERE d.kind = 'device' AND c.id = ANY($1) AND NOT (d.id = ANY($1))
		       AND d.group_id IS DISTINCT FROM $2::uuid AND c.group_id IS DISTINCT FROM $2::uuid)`,
		ids, newGroup).Scan(&away, &orphaned)
	if err != nil {
		return fmt.Errorf("comprobando colectores: %w", err)
	}
	switch {
	case away > 0:
		return &GroupRuleError{Kind: ErrCollectorGroupMismatch, Devices: away}
	case orphaned > 0:
		return &GroupRuleError{Kind: ErrCollectorHasDevices, Devices: orphaned}
	}
	// Cuotas del cliente (B7): solo cuentan los que de verdad ENTRAN al grupo nuevo (no tenian ya ese group_id);
	// re-fijar el mismo grupo, o quitarlo (newGroup nil), nunca suma. Un movimiento puede mezclar agentes y
	// dispositivos (alta masiva de acciones), cada uno contra su propia cuota.
	if newGroup != nil {
		var movingHosts, movingDevices int
		if err := tx.QueryRow(ctx,
			`SELECT
			   (SELECT count(*)::int FROM hosts WHERE id = ANY($1) AND kind = 'agent' AND group_id IS DISTINCT FROM $2::uuid),
			   (SELECT count(*)::int FROM hosts WHERE id = ANY($1) AND kind = 'device' AND group_id IS DISTINCT FROM $2::uuid)`,
			ids, newGroup).Scan(&movingHosts, &movingDevices); err != nil {
			return fmt.Errorf("contando hosts que cambian de grupo: %w", err)
		}
		if movingHosts > 0 {
			if err := checkGroupQuota(ctx, tx, *newGroup, QuotaHosts, movingHosts); err != nil {
				return err
			}
		}
		if movingDevices > 0 {
			if err := checkGroupQuota(ctx, tx, *newGroup, QuotaDevices, movingDevices); err != nil {
				return err
			}
		}
	}
	return nil
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
			if err := checkGroupMove(ctx, tx, b.IDs, b.GroupID); err != nil {
				return err
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
