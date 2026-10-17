package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"argos/server/internal/store"
)

const (
	purgeWorkerInterval = 2 * time.Second
	// maxParallelPurgeJobs coincide con el tope de bajas simultaneas del store (maxConcurrentPurges): mas trabajos
	// solo esperarian turno con una conexion del pool ocupada.
	maxParallelPurgeJobs = 2
	purgeJobsRetention   = 30 * 24 * time.Hour
	purgeCleanupEvery    = time.Hour
	// purgeFinishTimeout acota cerrar un trabajo y auditarlo aunque el proceso este apagandose.
	purgeFinishTimeout = 10 * time.Second
)

// RunPurgeWorker ejecuta las bajas encoladas hasta que ctx se cancela. Al arrancar devuelve a la cola las que quedaron
// en curso por un reinicio (repetirlas continua donde se quedaron). Un trabajo interrumpido por el apagado queda en
// curso y se reanuda en el siguiente arranque.
func (a *API) RunPurgeWorker(ctx context.Context) {
	if n, err := a.store.RequeueStalePurgeJobs(ctx, 0); err != nil {
		a.logger.Error("no se pudieron reencolar las bajas interrumpidas", "err", err)
	} else if n > 0 {
		a.logger.Info("bajas interrumpidas reencoladas", "count", n)
	}
	var wg sync.WaitGroup
	defer wg.Wait()
	slots := make(chan struct{}, maxParallelPurgeJobs)
	ticker := time.NewTicker(purgeWorkerInterval)
	defer ticker.Stop()
	var lastCleanup time.Time
	for {
		a.startPurgeJobs(ctx, &wg, slots)
		if now := a.now(); now.Sub(lastCleanup) >= purgeCleanupEvery {
			lastCleanup = now
			if _, err := a.store.DeleteOldPurgeJobs(ctx, purgeJobsRetention); err != nil && ctx.Err() == nil {
				a.logger.Error("no se pudieron borrar las bajas viejas", "err", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// startPurgeJobs reclama trabajos mientras haya cupo y los lanza cada uno en su goroutine (wg las espera al apagar).
func (a *API) startPurgeJobs(ctx context.Context, wg *sync.WaitGroup, slots chan struct{}) {
	for {
		select {
		case slots <- struct{}{}:
		default:
			return
		}
		job, ok, err := a.store.ClaimPurgeJob(ctx)
		if err != nil || !ok {
			<-slots
			if err != nil && ctx.Err() == nil {
				a.logger.Error("no se pudo tomar una baja de la cola", "err", err)
			}
			return
		}
		wg.Go(func() {
			defer func() { <-slots }()
			a.executePurgeJob(ctx, job)
		})
	}
}

// executePurgeJob hace la baja, cierra el trabajo y deja la auditoria (con el solicitante como actor).
func (a *API) executePurgeJob(ctx context.Context, job store.PurgeJob) {
	result, detail, err := a.runPurge(ctx, job)
	var body []byte
	if err == nil {
		if body, err = json.Marshal(result); err != nil {
			err = fmt.Errorf("serializando el resultado: %w", err)
		}
	}
	if err != nil && ctx.Err() != nil {
		a.logger.Warn("baja interrumpida por el apagado: se reanuda al arrancar", "job", job.ID, "kind", job.Kind)
		return
	}

	// Contexto propio: el trabajo termino (bien o mal) aunque el proceso este apagandose.
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), purgeFinishTimeout)
	defer cancel()
	e := store.AuditEntry{
		ActorUserID: job.RequestedBy, ActorUsername: job.RequestedByName, TargetID: job.TargetID,
		TargetType: job.Kind, Outcome: store.OutcomeOK,
	}
	switch job.Kind {
	case store.PurgeKindHost:
		e.Event = evHostPurged
	case store.PurgeKindGroup:
		e.Event = evGroupPurged
	default:
		e.Event = evDeviceDeleted
	}
	if err != nil {
		a.logger.Error("la baja fallo", "job", job.ID, "kind", job.Kind, "err", err)
		if ferr := a.store.FinishPurgeJob(fctx, job.ID, nil, purgeJobError(err)); ferr != nil {
			a.logger.Error("no se pudo cerrar la baja fallida", "job", job.ID, "err", ferr)
		}
		if detail != nil { // una baja de grupo interrumpida ya borro parte: queda constancia
			e.Outcome = store.OutcomeFail
			a.writeAudit(fctx, e, detail)
		}
		return
	}
	if ferr := a.store.FinishPurgeJob(fctx, job.ID, body, ""); ferr != nil {
		a.logger.Error("no se pudo cerrar la baja", "job", job.ID, "err", ferr)
	}
	a.writeAudit(fctx, e, detail)
}

// runPurge ejecuta el borrado del trabajo. Devuelve el resultado (lo que respondia la baja sincrona) y el detalle de
// auditoria; con error, el detalle solo va si hay algo que auditar.
func (a *API) runPurge(ctx context.Context, job store.PurgeJob) (result any, detail map[string]any, err error) {
	switch job.Kind {
	case store.PurgeKindHost:
		c, err := a.store.PurgeHost(ctx, job.TargetID, job.TargetName)
		if err != nil {
			return nil, nil, err
		}
		return map[string]any{"deleted_rows": toDeletedRowsDTO(c)}, map[string]any{"metrics_rows": c.Metrics}, nil
	case store.PurgeKindDevice:
		name, err := a.store.DeleteDevice(ctx, job.TargetID)
		if err != nil {
			return nil, nil, err
		}
		return map[string]any{}, map[string]any{"name": name}, nil
	case store.PurgeKindGroup:
		return a.runPurgeGroup(ctx, job)
	}
	return nil, nil, fmt.Errorf("tipo de baja desconocido: %q", job.Kind)
}

func (a *API) runPurgeGroup(ctx context.Context, job store.PurgeJob) (any, map[string]any, error) {
	res, err := a.store.PurgeGroup(ctx, job.TargetID, store.PurgeGroupOpts{
		ConfirmName: job.TargetName, DeleteGroup: job.Options.DeleteGroup, DisableUsers: job.Options.DisableUsers,
		OnProgress: func(done, total int) {
			if err := a.store.UpdatePurgeProgress(ctx, job.ID, done, total); err != nil && ctx.Err() == nil {
				a.logger.Warn("no se pudo guardar el avance de la baja", "job", job.ID, "err", err)
			}
		},
	})
	detail := purgeGroupAudit(res)
	if err != nil {
		if res.Agents+res.Devices > 0 || res.Rows.Metrics > 0 {
			detail["reason"] = "interrupted"
			return nil, detail, err
		}
		return nil, nil, err
	}
	failed := res.Failed
	if failed == nil {
		failed = []string{}
	}
	return map[string]any{
		"agents": res.Agents, "devices": res.Devices, "deleted_rows": toDeletedRowsDTO(res.Rows),
		"failed_host_ids": failed, "rules": res.Rules, "windows": res.Windows, "checks": res.Checks,
		"enroll_tokens": res.EnrollTokens, "channels": res.Channels, "snmp_profiles": res.SnmpProfiles,
		"users_disabled": res.UsersDisabled, "users_scope_reduced": res.UsersScopeReduced,
		"group_deleted": res.GroupDeleted,
		"group_name":    job.TargetName, "purged_at": a.now().UTC(), "performed_by": job.RequestedByName,
	}, detail, nil
}

// purgeJobError es el mensaje que ve el cliente en un trabajo fallido: corto y sin SQL ni rutas (el detalle va al log).
func purgeJobError(err error) string {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return "el destino ya no existe"
	case errors.Is(err, store.ErrPurgeConfirm):
		return "el nombre cambio: vuelve a pedir la baja"
	case errors.Is(err, store.ErrCollectorHasDevices):
		return "el host es colector de dispositivos que siguen vivos"
	default:
		return "la baja fallo; reintenta"
	}
}
