package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Bajas en segundo plano: la peticion HTTP valida y encola un PurgeJob; un worker lo ejecuta (PurgeHost, DeleteDevice o
// PurgeGroup, que ya son idempotentes: un trabajo interrumpido se reanuda) y el cliente consulta su progreso.

// Tipos y estados de un trabajo de baja.
const (
	PurgeKindHost   = "host"
	PurgeKindDevice = "device"
	PurgeKindGroup  = "group"

	PurgeJobQueued  = "queued"
	PurgeJobRunning = "running"
	PurgeJobDone    = "done"
	PurgeJobFailed  = "failed"
)

// ErrPurgeBusy: ya hay una baja de grupo activa (queued o running) de otro grupo; solo se admite una a la vez.
var ErrPurgeBusy = errors.New("hay otra baja de grupo en curso")

// purgeJobsLockKey es el lock advisory (de transaccion) que serializa la creacion de trabajos: la regla "una baja de
// grupo a la vez" y "un trabajo activo por destino" se comprueban y aplican sin carrera.
const purgeJobsLockKey = 4404401

// PurgeJobOptions son las opciones de una baja de grupo.
type PurgeJobOptions struct {
	DeleteGroup  bool `json:"delete_group"`
	DisableUsers bool `json:"disable_users"`
}

// PurgeJob es una baja encolada, en curso o terminada.
type PurgeJob struct {
	ID, Kind, TargetID, TargetName string
	Options                        PurgeJobOptions
	RequestedBy                    string // "" si el usuario se borro
	RequestedByName                string
	State                          string
	TotalHosts, DoneHosts          int
	Result                         []byte // JSON; nil hasta terminar bien
	Error                          *string
	CreatedAt                      time.Time
	StartedAt, FinishedAt          *time.Time
}

// NewPurgeJob son los datos de CreatePurgeJob. RequestedBy es opcional.
type NewPurgeJob struct {
	Kind, TargetID, TargetName string
	Options                    PurgeJobOptions
	RequestedBy                string
	RequestedByName            string
	TotalHosts                 int
}

const purgeJobCols = `id::text, kind, target_id::text, target_name, options, requested_by::text, requested_by_name, state,
	total_hosts, done_hosts, result, error, created_at, started_at, finished_at`

func scanPurgeJob(row pgx.Row) (PurgeJob, error) {
	var j PurgeJob
	var by *string
	if err := row.Scan(&j.ID, &j.Kind, &j.TargetID, &j.TargetName, &j.Options, &by, &j.RequestedByName, &j.State,
		&j.TotalHosts, &j.DoneHosts, &j.Result, &j.Error, &j.CreatedAt, &j.StartedAt, &j.FinishedAt); err != nil {
		return PurgeJob{}, err
	}
	if by != nil {
		j.RequestedBy = *by
	}
	return j, nil
}

// CreatePurgeJob encola una baja. Si ya hay un trabajo activo (queued o running) para ese destino devuelve ESE trabajo
// (idempotente). ErrPurgeBusy si es de grupo y hay otra baja de grupo activa de un grupo distinto.
func (s *Store) CreatePurgeJob(ctx context.Context, p NewPurgeJob) (PurgeJob, error) {
	var by *string
	if p.RequestedBy != "" {
		by = &p.RequestedBy
	}
	var job PurgeJob
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, purgeJobsLockKey); err != nil {
			return fmt.Errorf("bloqueando la cola de bajas: %w", err)
		}
		existing, err := scanPurgeJob(tx.QueryRow(ctx,
			`SELECT `+purgeJobCols+` FROM purge_jobs WHERE kind = $1 AND target_id = $2 AND state IN ('queued', 'running')`,
			p.Kind, p.TargetID))
		if err == nil {
			job = existing
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("buscando baja activa: %w", err)
		}
		if p.Kind == PurgeKindGroup {
			var busy bool
			if err := tx.QueryRow(ctx,
				`SELECT EXISTS (SELECT 1 FROM purge_jobs WHERE kind = 'group' AND state IN ('queued', 'running'))`).Scan(&busy); err != nil {
				return fmt.Errorf("buscando baja de grupo activa: %w", err)
			}
			if busy {
				return ErrPurgeBusy
			}
		}
		job, err = scanPurgeJob(tx.QueryRow(ctx,
			`INSERT INTO purge_jobs (kind, target_id, target_name, options, requested_by, requested_by_name, total_hosts)
			 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING `+purgeJobCols,
			p.Kind, p.TargetID, p.TargetName, p.Options, by, p.RequestedByName, p.TotalHosts))
		if err != nil {
			return fmt.Errorf("creando baja: %w", err)
		}
		return nil
	})
	if err != nil {
		return PurgeJob{}, err
	}
	return job, nil
}

// ClaimPurgeJob toma el trabajo en cola mas antiguo y lo pasa a running. Una baja de grupo no se toma mientras otra
// corre. ok es false si no hay nada que tomar.
func (s *Store) ClaimPurgeJob(ctx context.Context) (job PurgeJob, ok bool, err error) {
	job, err = scanPurgeJob(s.pool.QueryRow(ctx,
		`UPDATE purge_jobs SET state = 'running', started_at = now(), heartbeat_at = now()
		  WHERE id = (SELECT id FROM purge_jobs
		               WHERE state = 'queued'
		                 AND (kind <> 'group' OR NOT EXISTS (SELECT 1 FROM purge_jobs g WHERE g.kind = 'group' AND g.state = 'running'))
		               ORDER BY created_at, id LIMIT 1 FOR UPDATE SKIP LOCKED)
		  RETURNING `+purgeJobCols))
	if errors.Is(err, pgx.ErrNoRows) {
		return PurgeJob{}, false, nil
	}
	if err != nil {
		return PurgeJob{}, false, fmt.Errorf("tomando baja: %w", err)
	}
	return job, true, nil
}

// UpdatePurgeProgress fija el avance de un trabajo en curso y renueva su latido.
func (s *Store) UpdatePurgeProgress(ctx context.Context, id string, done, total int) error {
	if _, err := s.pool.Exec(ctx,
		`UPDATE purge_jobs SET done_hosts = $2, total_hosts = $3, heartbeat_at = now() WHERE id = $1 AND state = 'running'`,
		id, done, total); err != nil {
		return fmt.Errorf("actualizando avance de la baja: %w", err)
	}
	return nil
}

// FinishPurgeJob cierra un trabajo: con jobErr vacio queda done con result (JSON) y todo su avance; con jobErr queda
// failed con ese mensaje (corto, apto pa el cliente: el detalle va al log).
func (s *Store) FinishPurgeJob(ctx context.Context, id string, result []byte, jobErr string) error {
	var err error
	if jobErr == "" {
		_, err = s.pool.Exec(ctx,
			`UPDATE purge_jobs SET state = 'done', result = $2, error = NULL, done_hosts = total_hosts, finished_at = now() WHERE id = $1`,
			id, result)
	} else {
		_, err = s.pool.Exec(ctx,
			`UPDATE purge_jobs SET state = 'failed', error = $2, finished_at = now() WHERE id = $1`, id, jobErr)
	}
	if err != nil {
		return fmt.Errorf("cerrando baja: %w", err)
	}
	return nil
}

// RequeueStalePurgeJobs devuelve a la cola los trabajos running cuyo ultimo latido es mas viejo que olderThan (con 0,
// todos: se usa al arrancar, cuando ningun trabajo puede seguir corriendo). Devuelve cuantos.
func (s *Store) RequeueStalePurgeJobs(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE purge_jobs SET state = 'queued'
		  WHERE state = 'running' AND (heartbeat_at IS NULL OR heartbeat_at < now() - $1 * interval '1 second')`,
		olderThan.Seconds())
	if err != nil {
		return 0, fmt.Errorf("reencolando bajas: %w", err)
	}
	return tag.RowsAffected(), nil
}

// PurgeJobByID devuelve un trabajo; ErrNotFound si no existe.
func (s *Store) PurgeJobByID(ctx context.Context, id string) (PurgeJob, error) {
	j, err := scanPurgeJob(s.pool.QueryRow(ctx, `SELECT `+purgeJobCols+` FROM purge_jobs WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return PurgeJob{}, ErrNotFound
	}
	if err != nil {
		return PurgeJob{}, fmt.Errorf("leyendo baja: %w", err)
	}
	return j, nil
}

// ListPurgeJobs devuelve los trabajos mas recientes primero; con activeOnly, solo los queued y running.
func (s *Store) ListPurgeJobs(ctx context.Context, activeOnly bool, limit int) ([]PurgeJob, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+purgeJobCols+` FROM purge_jobs WHERE (NOT $1 OR state IN ('queued', 'running'))
		  ORDER BY created_at DESC, id LIMIT $2`, activeOnly, limit)
	if err != nil {
		return nil, fmt.Errorf("listando bajas: %w", err)
	}
	jobs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (PurgeJob, error) { return scanPurgeJob(row) })
	if err != nil {
		return nil, fmt.Errorf("leyendo bajas: %w", err)
	}
	return jobs, nil
}

// DeleteOldPurgeJobs borra los trabajos terminados hace mas de olderThan y devuelve cuantos.
func (s *Store) DeleteOldPurgeJobs(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM purge_jobs WHERE state IN ('done', 'failed') AND finished_at < now() - $1 * interval '1 second'`,
		olderThan.Seconds())
	if err != nil {
		return 0, fmt.Errorf("borrando bajas viejas: %w", err)
	}
	return tag.RowsAffected(), nil
}
