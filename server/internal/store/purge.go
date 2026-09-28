package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Purga (baja de un cliente): borra un host o todo un grupo con TODOS sus datos. Lo que NO limpia: los respaldos de la
// base (deploy/argos-backup.sh; se rotan a los 14 dias) ni audit_log (append-only: conserva ids de host y, en algunos
// eventos, su nombre hasta que vence su propia retencion). El resumen horario (metrics_hourly, hasta 400 dias) SI se
// borra.

// ErrPurgeConfirm: el nombre de confirmacion no coincide con el del host o grupo.
var ErrPurgeConfirm = errors.New("el nombre de confirmacion no coincide")

// PurgeCounts son las filas borradas de un host.
type PurgeCounts struct {
	Metrics, Hourly, Chunks, Disks, DiskChunks, Checks int64
}

func (c *PurgeCounts) add(o PurgeCounts) {
	c.Metrics += o.Metrics
	c.Hourly += o.Hourly
	c.Chunks += o.Chunks
	c.Disks += o.Disks
	c.DiskChunks += o.DiskChunks
	c.Checks += o.Checks
}

// purgeHostTx borra los datos de un host (agente o dispositivo) y el host. Las FK de metrics, metrics_hourly,
// service_checks y disk_samples a hosts no tienen CASCADE: se borran primero; el resto (host_info, host_latest,
// host_events, inventory_*, updates_*, alerts, alert_pending, service_check_state/events, reglas y ventanas con alcance
// a ese host, ...) cae por CASCADE al borrar la fila del host. Tambien borrar la fila invalida su token de agente.
// ErrNotFound si no existe; GroupRuleError (ErrCollectorHasDevices) si es colector de dispositivos que siguen vivos.
// tails acota, por tabla, hasta donde mira el borrado: si las tandas ya vaciaron una tabla grande, repetir aqui el
// DELETE por host recorreria todas sus entradas de indice ya muertas (con 10 M de filas, ~27 s en una transaccion);
// solo se revisa desde tails[tabla] (los ultimos dias, por si llego algo en vuelo). Sin entrada: borrado completo.
func purgeHostTx(ctx context.Context, tx pgx.Tx, hostID string, tails map[string]time.Time) (PurgeCounts, error) {
	var kind string
	err := tx.QueryRow(ctx, `SELECT kind FROM hosts WHERE id = $1 FOR UPDATE`, hostID).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return PurgeCounts{}, ErrNotFound
	}
	if err != nil {
		return PurgeCounts{}, fmt.Errorf("bloqueando host: %w", err)
	}
	var devices int
	if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM hosts WHERE collector_host_id = $1`, hostID).Scan(&devices); err != nil {
		return PurgeCounts{}, fmt.Errorf("contando dispositivos del colector: %w", err)
	}
	if devices > 0 {
		return PurgeCounts{}, &GroupRuleError{Kind: ErrCollectorHasDevices, Devices: devices}
	}
	var c PurgeCounts
	// Cada tabla en su sentencia y en este orden; metrics y service_checks son particionadas (el indice por
	// (host_id, ts) de cada particion resuelve el borrado).
	for _, d := range []struct {
		table string
		n     *int64
	}{
		{"metrics", &c.Metrics}, {"metrics_hourly", &c.Hourly}, {"metrics_chunks", &c.Chunks},
		{"disk_samples", &c.Disks}, {"disk_chunks", &c.DiskChunks}, {"service_checks", &c.Checks},
	} {
		// El nombre de tabla es una constante de esta funcion (no entrada de usuario).
		q, args := `DELETE FROM `+d.table+` WHERE host_id = $1`, []any{hostID}
		if from, ok := tails[d.table]; ok {
			q, args = q+` AND ts >= $2`, append(args, from)
		}
		tag, err := tx.Exec(ctx, q, args...)
		if err != nil {
			return PurgeCounts{}, fmt.Errorf("borrando %s del host: %w", d.table, err)
		}
		*d.n = tag.RowsAffected()
	}
	if _, err := tx.Exec(ctx, `DELETE FROM hosts WHERE id = $1`, hostID); err != nil {
		return PurgeCounts{}, fmt.Errorf("borrando host: %w", err)
	}
	return c, nil
}

// purgeBatchWindow es el tamano de cada tanda de borrado de las tablas grandes: un dia (una particion). Cada tanda va
// en su propia transaccion, asi un host con millones de filas nunca produce una transaccion (ni un WAL) gigante.
const purgeBatchWindow = 24 * time.Hour

// purgeHostBatched borra un host y todos sus datos sin una transaccion enorme: primero las tablas particionadas
// grandes por ventanas de un dia (cada una confirmada aparte) y al final, en una transaccion corta, lo que quede y el
// host. Si se interrumpe a mitad, el host sigue existiendo con menos historial y repetir la llamada continua donde
// quedo. Lo que puede rechazarse (colector con dispositivos vivos) se comprueba ANTES de borrar nada.
func (s *Store) purgeHostBatched(ctx context.Context, hostID string) (PurgeCounts, error) {
	select {
	case s.purgeSlots <- struct{}{}:
		defer func() { <-s.purgeSlots }()
	case <-ctx.Done():
		return PurgeCounts{}, fmt.Errorf("esperando turno de purga: %w", ctx.Err())
	}
	if err := s.rejectLiveCollector(ctx, hostID); err != nil {
		return PurgeCounts{}, err
	}
	// Deshabilitar primero: el agente pasa a recibir 401 y deja de escribir mientras se borra el historial.
	if _, err := s.pool.Exec(ctx, `UPDATE hosts SET status = 'disabled', status_changed_at = now() WHERE id = $1 AND status <> 'disabled'`, hostID); err != nil {
		return PurgeCounts{}, fmt.Errorf("deshabilitando host antes de borrarlo: %w", err)
	}
	var c PurgeCounts
	tails := map[string]time.Time{}
	for _, t := range []struct {
		table string
		n     *int64
	}{{"metrics", &c.Metrics}, {"service_checks", &c.Checks}, {"disk_samples", &c.Disks}} {
		n, tail, err := s.deleteHostRowsByDay(ctx, t.table, hostID)
		if err != nil {
			return PurgeCounts{}, err
		}
		*t.n = n
		if !tail.IsZero() {
			tails[t.table] = tail
		}
	}
	var rest PurgeCounts
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		rest, err = purgeHostTx(ctx, tx, hostID, tails)
		return err
	})
	if err != nil {
		return PurgeCounts{}, err
	}
	c.add(rest)
	return c, nil
}

// deleteHostRowsByDay borra las filas de un host de una tabla particionada por ts, un dia por sentencia. Devuelve las
// filas borradas y desde cuando debe revisar aun la transaccion final (los dos ultimos dias; cero si no habia filas).
func (s *Store) deleteHostRowsByDay(ctx context.Context, table, hostID string) (int64, time.Time, error) {
	var lo, hi *time.Time
	// El nombre de tabla es una constante de purgeHostBatched (no entrada de usuario).
	if err := s.pool.QueryRow(ctx, `SELECT min(ts), max(ts) FROM `+table+` WHERE host_id = $1`, hostID).Scan(&lo, &hi); err != nil {
		return 0, time.Time{}, fmt.Errorf("rango de %s del host: %w", table, err)
	}
	if lo == nil || hi == nil {
		return 0, time.Time{}, nil
	}
	var total int64
	last := lo.UTC().Truncate(purgeBatchWindow)
	for start := last; !start.After(*hi); start = start.Add(purgeBatchWindow) {
		if err := ctx.Err(); err != nil {
			return total, time.Time{}, fmt.Errorf("purga interrumpida: %w", err)
		}
		tag, err := s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE host_id = $1 AND ts >= $2 AND ts < $3`,
			hostID, start, start.Add(purgeBatchWindow))
		if err != nil {
			return total, time.Time{}, fmt.Errorf("borrando %s del host: %w", table, err)
		}
		total += tag.RowsAffected()
		last = start
	}
	return total, last.Add(-purgeBatchWindow), nil
}

// rejectLiveCollector devuelve un GroupRuleError (ErrCollectorHasDevices) si el host es colector de dispositivos vivos.
func (s *Store) rejectLiveCollector(ctx context.Context, hostID string) error {
	var devices int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)::int FROM hosts WHERE collector_host_id = $1`, hostID).Scan(&devices); err != nil {
		return fmt.Errorf("contando dispositivos del colector: %w", err)
	}
	if devices > 0 {
		return &GroupRuleError{Kind: ErrCollectorHasDevices, Devices: devices}
	}
	return nil
}

// PreparePurgeHost valida una baja de host sin borrar nada y devuelve su nombre. confirmHostname debe ser su nombre
// (ErrPurgeConfirm si no). ErrNotFound si no existe o es un dispositivo (esos se borran con DeleteDevice);
// ErrCollectorHasDevices mientras existan dispositivos que lo usen de colector.
func (s *Store) PreparePurgeHost(ctx context.Context, hostID, confirmHostname string) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, `SELECT hostname FROM hosts WHERE id = $1 AND kind = 'agent'`, hostID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("leyendo host: %w", err)
	}
	if name != confirmHostname {
		return "", ErrPurgeConfirm
	}
	if err := s.rejectLiveCollector(ctx, hostID); err != nil {
		return "", err
	}
	return name, nil
}

// PurgeHost borra un host con agente y todos sus datos, con las validaciones de PreparePurgeHost.
func (s *Store) PurgeHost(ctx context.Context, hostID, confirmHostname string) (PurgeCounts, error) {
	if _, err := s.PreparePurgeHost(ctx, hostID, confirmHostname); err != nil {
		return PurgeCounts{}, err
	}
	return s.purgeHostBatched(ctx, hostID)
}

// PurgeGroupOpts son las opciones de PurgeGroup.
type PurgeGroupOpts struct {
	// ConfirmName debe ser el nombre del grupo.
	ConfirmName string
	// DeleteGroup elimina el grupo si queda vacio.
	DeleteGroup bool
	// DisableUsers deshabilita a los viewers cuyo unico ambito era el grupo (y borra sus sesiones).
	DisableUsers bool
	// OnProgress, si no es nil, se llama tras cada host procesado con los hechos y el total (el trabajo en segundo plano
	// lo usa pa mostrar el avance). No debe bloquear.
	OnProgress func(done, total int)
}

// PurgeGroupResult son los conteos de PurgeGroup.
type PurgeGroupResult struct {
	Agents, Devices int
	Rows            PurgeCounts
	// Failed son los ids de los hosts que no se pudieron purgar (el resto siguio); volver a llamar reintenta solo esos.
	Failed []string

	Rules, Windows, Checks, EnrollTokens int
	// Channels: canales de notificacion propios del grupo (group_id = X; los globales, group_id NULL, nunca se tocan).
	// SnmpProfiles: perfiles SNMP de dispositivos del grupo que, tras purgarlos, quedaron sin ningun host que los use
	// (uno compartido con un dispositivo de otro grupo sobrevive).
	Channels, SnmpProfiles           int
	UsersDisabled, UsersScopeReduced int // UsersScopeReduced: viewers a los que se quito el grupo (incluye los deshabilitados)
	GroupDeleted                     bool
}

// purgeFailedCap acota los ids de fallos que se devuelven.
const purgeFailedCap = 100

// checkPurgeGroupConfirm: ErrNotFound si el grupo no existe; ErrPurgeConfirm si confirmName no es su nombre.
func (s *Store) checkPurgeGroupConfirm(ctx context.Context, groupID, confirmName string) error {
	var name string
	err := s.pool.QueryRow(ctx, `SELECT name FROM host_groups WHERE id = $1`, groupID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("leyendo grupo: %w", err)
	}
	if name != confirmName {
		return ErrPurgeConfirm
	}
	return nil
}

// PreparePurgeGroup valida una baja de grupo sin borrar nada y devuelve cuantos hosts y dispositivos tiene (el total
// del avance). Mismos errores que PurgeGroup.
func (s *Store) PreparePurgeGroup(ctx context.Context, groupID, confirmName string) (int, error) {
	if err := s.checkPurgeGroupConfirm(ctx, groupID, confirmName); err != nil {
		return 0, err
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)::int FROM hosts WHERE group_id = $1`, groupID).Scan(&total); err != nil {
		return 0, fmt.Errorf("contando hosts del grupo: %w", err)
	}
	return total, nil
}

// PurgeGroup purga todos los hosts y dispositivos del grupo (uno por transaccion: si uno falla, sigue y lo reporta en
// Failed), borra las reglas, ventanas, chequeos y enroll tokens del grupo, quita el grupo del ambito de los viewers
// (deshabilita a los que solo lo tenian a el si DisableUsers) y, con DeleteGroup y sin fallos, elimina el grupo.
// Idempotente. ErrNotFound si el grupo no existe; ErrPurgeConfirm si el nombre no coincide.
func (s *Store) PurgeGroup(ctx context.Context, groupID string, o PurgeGroupOpts) (PurgeGroupResult, error) {
	if err := s.checkPurgeGroupConfirm(ctx, groupID, o.ConfirmName); err != nil {
		return PurgeGroupResult{}, err
	}

	// Primero los dispositivos: los agentes que son su colector no se pueden borrar mientras existan.
	rows, err := s.pool.Query(ctx, `SELECT id::text, kind FROM hosts WHERE group_id = $1 ORDER BY (kind = 'device') DESC, id`, groupID)
	if err != nil {
		return PurgeGroupResult{}, fmt.Errorf("listando hosts del grupo: %w", err)
	}
	type target struct{ id, kind string }
	targets, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (target, error) {
		var t target
		err := row.Scan(&t.id, &t.kind)
		return t, err
	})
	if err != nil {
		return PurgeGroupResult{}, fmt.Errorf("leyendo hosts del grupo: %w", err)
	}

	// Perfiles SNMP de los dispositivos del grupo: se recolectan antes de purgar los hosts (snmp_profiles no tiene
	// group_id, solo se llega a ellos por hosts.snmp_profile_id) para poder borrar despues los que queden sin uso.
	profileRows, err := s.pool.Query(ctx,
		`SELECT DISTINCT snmp_profile_id::text FROM hosts WHERE group_id = $1 AND snmp_profile_id IS NOT NULL`, groupID)
	if err != nil {
		return PurgeGroupResult{}, fmt.Errorf("listando perfiles snmp del grupo: %w", err)
	}
	profileIDs, err := pgx.CollectRows(profileRows, pgx.RowTo[string])
	if err != nil {
		return PurgeGroupResult{}, fmt.Errorf("leyendo perfiles snmp del grupo: %w", err)
	}

	var res PurgeGroupResult
	for i, t := range targets {
		if err := ctx.Err(); err != nil {
			return res, fmt.Errorf("purga interrumpida: %w", err)
		}
		c, err := s.purgeHostBatched(ctx, t.id)
		switch {
		case errors.Is(err, ErrNotFound): // ya lo borro otro (idempotente)
		case err != nil:
			if len(res.Failed) < purgeFailedCap {
				res.Failed = append(res.Failed, t.id)
			}
		default:
			res.Rows.add(c)
			if t.kind == "device" {
				res.Devices++
			} else {
				res.Agents++
			}
		}
		if o.OnProgress != nil {
			o.OnProgress(i+1, len(targets))
		}
	}

	if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return purgeGroupConfig(ctx, tx, groupID, profileIDs, o, &res)
	}); err != nil {
		return res, err
	}
	if o.DeleteGroup && len(res.Failed) == 0 {
		switch err := s.DeleteGroup(ctx, groupID); {
		case err == nil:
			res.GroupDeleted = true
		case errors.Is(err, ErrGroupNotEmpty), errors.Is(err, ErrNotFound): // llego un host nuevo, o ya no estaba
		default:
			return res, err
		}
	}
	return res, nil
}

// purgeGroupConfig borra la configuracion con alcance solo a ese grupo y ajusta a los viewers. profileIDs son los
// perfiles SNMP recolectados de los dispositivos del grupo (antes de purgarlos); aqui, con los hosts ya purgados, se
// borran los que quedaron sin ningun host que los use (uno compartido con un dispositivo de otro grupo sobrevive).
func purgeGroupConfig(ctx context.Context, tx pgx.Tx, groupID string, profileIDs []string, o PurgeGroupOpts, res *PurgeGroupResult) error {
	for _, d := range []struct {
		sql string
		n   *int
	}{
		{`DELETE FROM alert_rules WHERE scope_type = 'group' AND scope_group_id = $1`, &res.Rules},
		{`DELETE FROM maintenance_windows WHERE scope_type = 'group' AND scope_group_id = $1`, &res.Windows},
		{`DELETE FROM service_check_defs WHERE scope_type = 'group' AND scope_group_id = $1`, &res.Checks},
		// Un enroll token del grupo enrolaria un host nuevo (sin grupo, al borrarse el grupo por SET NULL).
		{`DELETE FROM enroll_tokens WHERE group_id = $1`, &res.EnrollTokens},
		// Canales de notificacion propios del grupo; los globales (group_id NULL) nunca se tocan.
		{`DELETE FROM notification_channels WHERE group_id = $1`, &res.Channels},
	} {
		tag, err := tx.Exec(ctx, d.sql, groupID)
		if err != nil {
			return fmt.Errorf("borrando configuracion del grupo: %w", err)
		}
		*d.n = int(tag.RowsAffected())
	}

	if len(profileIDs) > 0 {
		tag, err := tx.Exec(ctx,
			`DELETE FROM snmp_profiles WHERE id = ANY($1) AND NOT EXISTS (SELECT 1 FROM hosts WHERE hosts.snmp_profile_id = snmp_profiles.id)`,
			profileIDs)
		if err != nil {
			return fmt.Errorf("borrando perfiles snmp sin uso: %w", err)
		}
		res.SnmpProfiles = int(tag.RowsAffected())
	}

	// Viewers restringidos con el grupo en su ambito: solo-este-grupo vs. tambien otros.
	rows, err := tx.Query(ctx,
		`SELECT u.id::text, NOT EXISTS (SELECT 1 FROM user_groups o WHERE o.user_id = u.id AND o.group_id <> $1)
		   FROM users u JOIN user_groups ug ON ug.user_id = u.id AND ug.group_id = $1
		  WHERE u.role = 'viewer' AND NOT u.all_groups ORDER BY u.id FOR UPDATE OF u`, groupID)
	if err != nil {
		return fmt.Errorf("listando viewers del grupo: %w", err)
	}
	type viewer struct {
		id   string
		only bool
	}
	viewers, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (viewer, error) {
		var v viewer
		err := row.Scan(&v.id, &v.only)
		return v, err
	})
	if err != nil {
		return fmt.Errorf("leyendo viewers del grupo: %w", err)
	}
	var onlyIDs, allIDs []string
	for _, v := range viewers {
		allIDs = append(allIDs, v.id)
		if v.only {
			onlyIDs = append(onlyIDs, v.id)
		}
	}
	if len(allIDs) > 0 {
		// Todos pierden el grupo: sin esta fila el ambito ya no lo incluye (a la siguiente solicitud), exista o no el grupo.
		if _, err := tx.Exec(ctx, `DELETE FROM user_groups WHERE group_id = $1`, groupID); err != nil {
			return fmt.Errorf("quitando el grupo del ambito de los viewers: %w", err)
		}
		res.UsersScopeReduced = len(allIDs)
	}
	if o.DisableUsers && len(onlyIDs) > 0 {
		tag, err := tx.Exec(ctx, `UPDATE users SET disabled_at = now() WHERE id = ANY($1) AND disabled_at IS NULL`, onlyIDs)
		if err != nil {
			return fmt.Errorf("deshabilitando viewers: %w", err)
		}
		res.UsersDisabled = int(tag.RowsAffected())
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = ANY($1)`, onlyIDs); err != nil {
			return fmt.Errorf("revocando sesiones: %w", err)
		}
	}
	return nil
}
