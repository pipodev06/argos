import { useEffect, useState } from 'react'
import { isPurgeJobFinal, pollPurgeJob } from '../services/purgeJobPoller'
import type { PurgeJob } from '../types/purge.types'

/**
 * Sigue un trabajo de baja: parte del `seed` (respuesta 202) y lo consulta cada 2 s hasta done/failed.
 * Limpia el timer al desmontar o al cambiar de trabajo. `error` solo con fallos definitivos.
 */
export function usePurgeJob(seed: PurgeJob | null) {
  const [polled, setPolled] = useState<PurgeJob | null>(null)
  const [fatal, setFatal] = useState<{ id: string; message: string } | null>(null)
  const id = seed?.id ?? null
  const seedFinal = seed ? isPurgeJobFinal(seed) : true

  useEffect(() => {
    if (!id || seedFinal) return
    return pollPurgeJob(id, { onUpdate: setPolled, onFatal: (message) => setFatal({ id, message }) })
  }, [id, seedFinal])

  const job = polled && polled.id === id ? polled : seed
  return { job, error: fatal && fatal.id === id ? fatal.message : null }
}
