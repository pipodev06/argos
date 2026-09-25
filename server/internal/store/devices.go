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

// Dispositivos sin agente (hosts.kind = 'device'): un equipo con IP que sondea un colector (un agente).
var (
	// ErrDeviceNotFound: el dispositivo referenciado (en el alcance device de un chequeo) no existe.
	ErrDeviceNotFound = errors.New("dispositivo no encontrado")
	// ErrScopeHostNotFound: el host referenciado en el alcance host de un chequeo no existe o no es un agente.
	ErrScopeHostNotFound = errors.New("host del alcance no encontrado")
	// ErrCollectorInvalid: el colector no existe o no es un agente.
	ErrCollectorInvalid = errors.New("el colector debe ser un host con agente")
	// ErrDeviceExists: ya hay un dispositivo con esa direccion en ese colector.
	ErrDeviceExists = errors.New("ya existe un dispositivo con esa direccion en ese colector")
	// ErrTooManyDevices: ya hay MaxDevices dispositivos.
	ErrTooManyDevices = errors.New("demasiados dispositivos")
)

// MaxDevices es el maximo de dispositivos.
const MaxDevices = 1000

const (
	deviceAddrKey      = "hosts_device_address_key"
	deviceCollectorFK  = "hosts_collector_host_id_fkey"
	deviceGroupFKey    = "hosts_group_id_fkey"
	maxDeviceListLimit = 100
)

// Device es un dispositivo sin agente.
type Device struct {
	ID              string
	Name            string
	Address         netip.Addr
	CollectorID     string
	CollectorName   string
	Group           *GroupRef
	Status          string
	StatusChangedAt time.Time
	LastSeenAt      *time.Time
	CreatedAt       time.Time
	// Profile es el perfil SNMP asignado (nil = ninguno).
	Profile *SNMPProfileRef
	// CollectorReachable es false si el colector no esta up: el estado del dispositivo no se esta evaluando.
	CollectorReachable bool

	lname string // lower(nombre) tal como lo calcula Postgres: clave del cursor
}

// SNMPProfileRef es la referencia corta a un perfil SNMP (sin secretos).
type SNMPProfileRef struct {
	ID      string
	Name    string
	Version int
}

// DeviceChange son los campos de un dispositivo que el admin puede cambiar (nil = no cambia).
type DeviceChange struct {
	Name        *string
	Address     *netip.Addr
	CollectorID *string
	// SetGroup indica que GroupID (nil = sin grupo) reemplaza al grupo actual.
	SetGroup bool
	GroupID  *string
	// SetProfile indica que ProfileID (nil = sin perfil) reemplaza al perfil SNMP actual.
	SetProfile bool
	ProfileID  *string
}

const deviceSelect = `
SELECT h.id::text, h.hostname, h.address, h.collector_host_id::text, c.hostname, g.id::text, g.name, h.status,
       h.status_changed_at, h.last_seen_at, h.created_at, c.status = 'up', lower(h.hostname), sp.id::text, sp.name, sp.version
  FROM hosts h
  JOIN hosts c ON c.id = h.collector_host_id
  LEFT JOIN snmp_profiles sp ON sp.id = h.snmp_profile_id
  LEFT JOIN host_groups g ON g.id = h.group_id`

func scanDevice(row pgx.Row) (Device, error) {
	var d Device
	var groupID, groupName, profID, profName *string
	var profVer *int16
	var addr netip.Prefix
	err := row.Scan(&d.ID, &d.Name, &addr, &d.CollectorID, &d.CollectorName, &groupID, &groupName, &d.Status,
		&d.StatusChangedAt, &d.LastSeenAt, &d.CreatedAt, &d.CollectorReachable, &d.lname, &profID, &profName, &profVer)
	if err != nil {
		return Device{}, err
	}
	d.Address = addr.Addr()
	if profID != nil && profName != nil && profVer != nil {
		d.Profile = &SNMPProfileRef{ID: *profID, Name: *profName, Version: int(*profVer)}
	}
	if groupID != nil && groupName != nil {
		d.Group = &GroupRef{ID: *groupID, Name: *groupName}
	}
	return d, nil
}

// DevicePage es una pagina de dispositivos; Total cuenta todos los del ambito.
type DevicePage struct {
	Items      []Device
	NextCursor *HostCursor
	Total      int
}

// ListDevices devuelve los dispositivos del ambito por nombre e id, con cursor keyset (LName e ID de HostCursor).
// limit se acota a 1..maxDeviceListLimit.
func (s *Store) ListDevices(ctx context.Context, scope Scope, f HostFilter, after *HostCursor, limit int) (DevicePage, error) {
	limit = min(max(limit, 1), maxDeviceListLimit)
	var args argList
	where := "h.kind = 'device' AND " + scope.groupCond("h.group_id", &args)
	// Filtros de la lista (mismos que GET /hosts): nombre, estado y grupo (o sin grupo).
	if f.Q != "" {
		where += fmt.Sprintf(` AND strpos(lower(h.hostname), lower(%s)) > 0`, args.add(f.Q))
	}
	if len(f.Statuses) > 0 {
		where += fmt.Sprintf(` AND h.status = ANY(%s::text[])`, args.add(f.Statuses))
	}
	var groupConds []string
	if len(f.GroupIDs) > 0 {
		groupConds = append(groupConds, fmt.Sprintf(`h.group_id = ANY(%s::uuid[])`, args.add(f.GroupIDs)))
	}
	if f.NoGroup {
		groupConds = append(groupConds, `h.group_id IS NULL`)
	}
	if len(groupConds) > 0 {
		where += " AND (" + strings.Join(groupConds, " OR ") + ")"
	}
	totalArgs := append(argList(nil), args...)
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)::int FROM hosts h WHERE `+where, []any(totalArgs)...).Scan(&total); err != nil {
		return DevicePage{}, fmt.Errorf("contando dispositivos: %w", err)
	}
	if after != nil {
		ln, id := args.add(after.LName), args.add(after.ID)
		where += fmt.Sprintf(` AND (lower(h.hostname), h.id) > (%s::text, %s::uuid)`, ln, id)
	}
	rows, err := s.pool.Query(ctx, deviceSelect+` WHERE `+where+
		fmt.Sprintf(` ORDER BY lower(h.hostname), h.id LIMIT %d`, limit+1), []any(args)...)
	if err != nil {
		return DevicePage{}, fmt.Errorf("listando dispositivos: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Device, error) { return scanDevice(row) })
	if err != nil {
		return DevicePage{}, fmt.Errorf("leyendo dispositivos: %w", err)
	}
	page := DevicePage{Items: items, Total: total}
	if len(items) > limit {
		page.Items = items[:limit]
		last := page.Items[limit-1]
		page.NextCursor = &HostCursor{LName: last.lname, ID: last.ID}
	}
	if page.Items == nil {
		page.Items = []Device{}
	}
	return page, nil
}

// DeviceByID devuelve un dispositivo. ErrNotFound si no existe, no es un dispositivo o esta fuera del ambito
// (indistinguibles).
func (s *Store) DeviceByID(ctx context.Context, scope Scope, id string) (Device, error) {
	args := argList{id}
	d, err := scanDevice(s.pool.QueryRow(ctx,
		deviceSelect+` WHERE h.id = $1 AND h.kind = 'device' AND `+scope.groupCond("h.group_id", &args), []any(args)...))
	if errors.Is(err, pgx.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	if err != nil {
		return Device{}, fmt.Errorf("leyendo dispositivo: %w", err)
	}
	return d, nil
}

func mapDeviceErr(err error, what string) error {
	switch {
	case isUniqueViolation(err, deviceAddrKey):
		return ErrDeviceExists
	case isFKViolation(err, deviceCollectorFK):
		return ErrCollectorInvalid
	case isFKViolation(err, deviceGroupFKey):
		return ErrGroupNotFound
	case isFKViolation(err, hostProfileFK):
		return ErrProfileNotFound
	}
	return fmt.Errorf("%s: %w", what, err)
}

// requireAgent devuelve ErrCollectorInvalid si id no es un host con agente activo (no deshabilitado).
func requireAgent(ctx context.Context, tx pgx.Tx, id string) error {
	var ok bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM hosts WHERE id = $1 AND kind = 'agent' AND status <> 'disabled')`, id).Scan(&ok); err != nil {
		return fmt.Errorf("buscando colector: %w", err)
	}
	if !ok {
		return ErrCollectorInvalid
	}
	return nil
}

// CreateDevice crea un dispositivo en estado pending. La direccion ya viene validada por quien llama.
// ErrCollectorInvalid, ErrDeviceExists, ErrGroupNotFound o ErrTooManyDevices segun el caso.
func (s *Store) CreateDevice(ctx context.Context, name string, addr netip.Addr, collectorID string, groupID *string) (Device, error) {
	var id string
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('argos.devices.cap'))`); err != nil {
			return fmt.Errorf("lock de dispositivos: %w", err)
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM hosts WHERE kind = 'device'`).Scan(&n); err != nil {
			return fmt.Errorf("contando dispositivos: %w", err)
		}
		if n >= MaxDevices {
			return ErrTooManyDevices
		}
		if err := requireAgent(ctx, tx, collectorID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx,
			`INSERT INTO hosts (hostname, kind, address, collector_host_id, group_id, status)
			 VALUES ($1, 'device', $2, $3, $4, 'pending') RETURNING id::text`,
			name, netip.PrefixFrom(addr, addr.BitLen()), collectorID, groupID).Scan(&id); err != nil {
			return mapDeviceErr(err, "insertando dispositivo")
		}
		return nil
	})
	if err != nil {
		return Device{}, err
	}
	return s.DeviceByID(ctx, AllScope(), id)
}

// UpdateDevice aplica el cambio. Cambiar la direccion o el colector devuelve el dispositivo a pending (todavia no
// se sabe si responde desde el nuevo origen). ErrNotFound si no existe o no es un dispositivo.
func (s *Store) UpdateDevice(ctx context.Context, id string, ch DeviceChange) (Device, error) {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var cur struct {
			name      string
			addr      netip.Prefix
			collector string
			group     *string
			profile   *string
			status    string
		}
		err := tx.QueryRow(ctx,
			`SELECT hostname, address, collector_host_id::text, group_id::text, snmp_profile_id::text, status
			   FROM hosts WHERE id = $1 AND kind = 'device' FOR UPDATE`, id).
			Scan(&cur.name, &cur.addr, &cur.collector, &cur.group, &cur.profile, &cur.status)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("leyendo dispositivo: %w", err)
		}
		name, addr, collector, group, profile, status := cur.name, cur.addr, cur.collector, cur.group, cur.profile, cur.status
		if ch.Name != nil {
			name = *ch.Name
		}
		moved := false
		if ch.Address != nil {
			p := netip.PrefixFrom(*ch.Address, ch.Address.BitLen())
			moved = moved || p != addr
			addr = p
		}
		if ch.CollectorID != nil {
			if err := requireAgent(ctx, tx, *ch.CollectorID); err != nil {
				return err
			}
			moved = moved || *ch.CollectorID != collector
			collector = *ch.CollectorID
		}
		if ch.SetGroup {
			group = ch.GroupID
		}
		if ch.SetProfile {
			profile = ch.ProfileID
		}
		if moved && status != "disabled" {
			status = "pending"
		}
		if _, err := tx.Exec(ctx,
			`UPDATE hosts SET hostname = $2, address = $3, collector_host_id = $4, group_id = $5,
			        status = $6, status_changed_at = CASE WHEN status <> $6 THEN now() ELSE status_changed_at END,
				        snmp_profile_id = $7
			  WHERE id = $1`, id, name, addr, collector, group, status, profile); err != nil {
			return mapDeviceErr(err, "actualizando dispositivo")
		}
		if status != cur.status {
			if _, err := tx.Exec(ctx,
				`INSERT INTO host_events (host_id, from_status, to_status, source) VALUES ($1, $2, $3, 'admin')`,
				id, cur.status, status); err != nil {
				return fmt.Errorf("registrando evento del dispositivo: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return Device{}, err
	}
	return s.DeviceByID(ctx, AllScope(), id)
}

// DeleteDevice borra el dispositivo (su estado, eventos y alertas caen en cascada) y devuelve su nombre.
// ErrNotFound si no existe o no es un dispositivo.
func (s *Store) DeleteDevice(ctx context.Context, id string) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, `DELETE FROM hosts WHERE id = $1 AND kind = 'device' RETURNING hostname`, id).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("borrando dispositivo: %w", err)
	}
	return name, nil
}

// requireScopeHost comprueba que el alcance de un chequeo que apunta a un host apunte a uno que existe: un
// dispositivo para el alcance device y un host con agente para el alcance host.
func requireScopeHost(ctx context.Context, tx pgx.Tx, sc AlertScope) error {
	var kind string
	var notFound error
	switch sc.Type {
	case ScopeDevice:
		kind, notFound = "device", ErrDeviceNotFound
	case ScopeHost:
		kind, notFound = "agent", ErrScopeHostNotFound
	default:
		return nil
	}
	if sc.HostID == nil {
		return nil
	}
	var ok bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM hosts WHERE id = $1 AND kind = $2)`, *sc.HostID, kind).Scan(&ok); err != nil {
		return fmt.Errorf("buscando host del alcance: %w", err)
	}
	if !ok {
		return notFound
	}
	return nil
}
