import { usePolledFetch } from '@/hooks/usePolledFetch'
import { getSystemStatus } from '../services/systemStatus.service'

// Refresco cada 30 s; usePolledFetch se pausa con la pestaña oculta.
const options = { intervalMs: 30_000 }

export function useSystemStatus() {
  return usePolledFetch(getSystemStatus, options)
}
