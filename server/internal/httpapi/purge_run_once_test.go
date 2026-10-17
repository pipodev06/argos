package httpapi

import "context"

// runPurgeJobsOnce ejecuta, una tras otra, las bajas en cola hasta vaciarla (los tests lo usan en vez del worker).
func (a *API) runPurgeJobsOnce(ctx context.Context) error {
	for {
		job, ok, err := a.store.ClaimPurgeJob(ctx)
		if err != nil || !ok {
			return err
		}
		a.executePurgeJob(ctx, job)
	}
}
