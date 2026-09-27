package store

import (
	"context"
	"errors"
	"fmt"

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
func purgeHostTx(ctx context.Context, tx pgx.Tx, hostID string) (PurgeCounts, error) {
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
		tag, err := tx.Exec(ctx, `DELETE FROM `+d.table+` WHERE host_id = $1`, hostID)
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

// PurgeHost borra un host con agente y todos sus datos. confirmHostname debe ser su nombre (ErrPurgeConfirm si no).
// ErrNotFound si no existe o es un dispositivo (esos se borran con DeleteDevice); ErrCollectorHasDevices mientras
// existan dispositivos que lo usen de colector.
func (s *Store) PurgeHost(ctx context.Context, hostID, confirmHostname string) (PurgeCounts, error) {
	var out PurgeCounts
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var name string
		err := tx.QueryRow(ctx, `SELECT hostname FROM hosts WHERE id = $1 AND kind = 'agent'`, hostID).Scan(&name)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("leyendo host: %w", err)
		}
		if name != confirmHostname {
			return ErrPurgeConfirm
		}
		out, err = purgeHostTx(ctx, tx, hostID)
		return err
	})
	if err != nil {
		return PurgeCounts{}, err
	}
	return out, nil
}

// PurgeGroupOpts son las opciones de PurgeGroup.
type PurgeGroupOpts struct {
	// ConfirmName debe ser el nombre del grupo.
	ConfirmName string
	// DeleteGroup elimina el grupo si queda vacio.
	DeleteGroup bool
	// DisableUsers deshabilita a los viewers cuyo unico ambito era el grupo (y borra sus sesiones).
	DisableUsers bool
}

// PurgeGroupResult son los conteos de PurgeGroup.
type PurgeGroupResult struct {
	Agents, Devices int
	Rows            PurgeCounts
	// Failed son los ids de los hosts que no se pudieron purgar (el resto siguio); volver a llamar reintenta solo esos.
	Failed []string

	Rules, Windows, Checks, EnrollTokens int
	UsersDisabled, UsersScopeReduced     int // UsersScopeReduced: viewers a los que se quito el grupo (incluye los deshabilitados)
	GroupDeleted                         bool
}

// purgeFailedCap acota los ids de fallos que se devuelven.
const purgeFailedCap = 100

// PurgeGroup purga todos los hosts y dispositivos del grupo (uno por transaccion: si uno falla, sigue y lo reporta en
// Failed), borra las reglas, ventanas, chequeos y enroll tokens del grupo, quita el grupo del ambito de los viewers
// (deshabilita a los que solo lo tenian a el si DisableUsers) y, con DeleteGroup y sin fallos, elimina el grupo.
// Idempotente. ErrNotFound si el grupo no existe; ErrPurgeConfirm si el nombre no coincide.
func (s *Store) PurgeGroup(ctx context.Context, groupID string, o PurgeGroupOpts) (PurgeGroupResult, error) {
	var name string
	err := s.pool.QueryRow(ctx, `SELECT name FROM host_groups WHERE id = $1`, groupID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return PurgeGroupResult{}, ErrNotFound
	}
	if err != nil {
		return PurgeGroupResult{}, fmt.Errorf("leyendo grupo: %w", err)
	}
	if name != o.ConfirmName {
		return PurgeGroupResult{}, ErrPurgeConfirm
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

	var res PurgeGroupResult
	for _, t := range targets {
		if err := ctx.Err(); err != nil {
			return res, fmt.Errorf("purga interrumpida: %w", err)
		}
		var c PurgeCounts
		err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			var err error
			c, err = purgeHostTx(ctx, tx, t.id)
			return err
		})
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
	}

	if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return purgeGroupConfig(ctx, tx, groupID, o, &res) }); err != nil {
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

// purgeGroupConfig borra la configuracion con alcance solo a ese grupo y ajusta a los viewers.
func purgeGroupConfig(ctx context.Context, tx pgx.Tx, groupID string, o PurgeGroupOpts, res *PurgeGroupResult) error {
	for _, d := range []struct {
		sql string
		n   *int
	}{
		{`DELETE FROM alert_rules WHERE scope_type = 'group' AND scope_group_id = $1`, &res.Rules},
		{`DELETE FROM maintenance_windows WHERE scope_type = 'group' AND scope_group_id = $1`, &res.Windows},
		{`DELETE FROM service_check_defs WHERE scope_type = 'group' AND scope_group_id = $1`, &res.Checks},
		// Un enroll token del grupo enrolaria un host nuevo (sin grupo, al borrarse el grupo por SET NULL).
		{`DELETE FROM enroll_tokens WHERE group_id = $1`, &res.EnrollTokens},
	} {
		tag, err := tx.Exec(ctx, d.sql, groupID)
		if err != nil {
			return fmt.Errorf("borrando configuracion del grupo: %w", err)
		}
		*d.n = int(tag.RowsAffected())
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
