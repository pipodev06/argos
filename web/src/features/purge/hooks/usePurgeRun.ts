import { useState } from 'react'
import { useUserMutation } from '@/features/users/hooks/useUserMutation'
import type { ApiError } from '@/lib/api'
import { isPurgeJobFinal, watchPurgeJobInBackground } from '../services/purgeJobPoller'
import type { PurgeJob } from '../types/purge.types'
import { usePurgeJob } from './usePurgeJob'

/**
 * Flujo comun de una baja en segundo plano: `start` valida y crea el trabajo (202), luego se sigue su avance.
 * `close` cierra el dialogo: si el trabajo sigue en curso no se cancela y, al terminar, se llama `onFinished`
 * (refrescar listas); si ya termino, `onFinished` se llama en el acto.
 */
export function usePurgeRun(
  create: () => Promise<PurgeJob>,
  mapError: ((err: ApiError) => string | null) | undefined,
  onFinished: () => void,
  onClose: () => void,
) {
  const [seed, setSeed] = useState<PurgeJob | null>(null)
  const mutation = useUserMutation()
  const { job, error: pollError } = usePurgeJob(seed)

  async function start() {
    await mutation.run(async () => {
      setSeed(await create())
    }, mapError)
  }

  function close() {
    if (!job) return onClose()
    if (isPurgeJobFinal(job)) return onFinished()
    watchPurgeJobInBackground(job.id, onFinished)
    onClose()
  }

  return { job, pollError, busy: mutation.busy, error: mutation.error, start, close }
}
