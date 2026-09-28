import { ApiError, isUnauthenticated } from '@/lib/api'
import { getPurgeJob } from './purge.service'
import type { PurgeJob } from '../types/purge.types'

export const PURGE_POLL_MS = 2000

export function isPurgeJobFinal(job: Pick<PurgeJob, 'state'>): boolean {
  return job.state === 'done' || job.state === 'failed'
}

interface PollOptions {
  onUpdate: (job: PurgeJob) => void
  /** Error definitivo (401, 400, 404): no se reintenta. Los transitorios (red, 5xx) se reintentan solos. */
  onFatal?: (message: string) => void
  intervalMs?: number
}

/**
 * Consulta el trabajo cada `intervalMs` hasta que termina (done/failed), sin solapar peticiones.
 * Devuelve la funcion que cancela (aborta la peticion y limpia el timer).
 */
export function pollPurgeJob(id: string, { onUpdate, onFatal, intervalMs = PURGE_POLL_MS }: PollOptions): () => void {
  let timer: ReturnType<typeof setTimeout> | null = null
  const controller = new AbortController()

  const schedule = () => {
    timer = setTimeout(() => void tick(), intervalMs)
  }

  async function tick() {
    timer = null
    try {
      const job = await getPurgeJob(id, controller.signal)
      if (controller.signal.aborted) return
      onUpdate(job)
      if (!isPurgeJobFinal(job)) schedule()
    } catch (err) {
      if (controller.signal.aborted) return
      const fatal = isUnauthenticated(err) || (err instanceof ApiError && (err.status === 400 || err.status === 404))
      if (fatal) onFatal?.(err instanceof Error ? err.message : 'Error desconocido')
      else schedule()
    }
  }

  schedule()
  return () => {
    controller.abort()
    if (timer !== null) clearTimeout(timer)
    timer = null
  }
}

/** Sigue un trabajo tras cerrar el dialogo: al terminar llama `onFinished` una vez (para refrescar listas). Se detiene sola. */
export function watchPurgeJobInBackground(id: string, onFinished: () => void): void {
  pollPurgeJob(id, {
    onUpdate: (job) => {
      if (isPurgeJobFinal(job)) onFinished()
    },
  })
}
