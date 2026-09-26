import { usePolledFetch } from '@/hooks/usePolledFetch'
import { getRetention } from '../services/retention.service'

// Una carga al montar y otra tras guardar (`refresh`): la retencion casi no cambia.
const options = { intervalMs: 60_000, paused: true }

export function useRetention() {
  return usePolledFetch(getRetention, options)
}
