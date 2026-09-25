import { usePolledFetch } from '@/hooks/usePolledFetch'
import { ApiError } from '@/lib/api'
import { listChannels } from '../services/channels.service'

// Un viewer restringido recibe 403: repetir la consulta no lo arregla.
const stopOn = (err: unknown) => err instanceof ApiError && err.status === 403
const options = { intervalMs: 30_000, stopOn }

export function useChannels() {
  return usePolledFetch(listChannels, options)
}
