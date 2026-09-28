import { useCallback } from 'react'
import { usePolledFetch } from '@/hooks/usePolledFetch'
import { getGroupUsage } from '../services/groups.service'

/** Consumo actual de cuotas del cliente (B7); sin refresco automático (se ve al abrir el formulario). */
export function useGroupUsage(groupId: string | null) {
  const fetcher = useCallback(
    (signal: AbortSignal) => {
      if (!groupId) return Promise.resolve(null)
      return getGroupUsage(groupId, signal)
    },
    [groupId],
  )
  return usePolledFetch(fetcher, { intervalMs: 30_000, paused: true })
}
