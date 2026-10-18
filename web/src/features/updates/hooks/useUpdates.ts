import { useCallback } from 'react'
import { isClientError, usePolledFetch } from '@/hooks/usePolledFetch'
import { getHostUpdates, listFleetUpdates } from '../services/updates.service'
import { buildFleetQuery, type FleetFilters } from '../utils/fleetQuery'

// Los parches cambian poco: refresco lento (pausado con la pestana oculta).
const REFRESH_MS = 60_000
const HISTORY_POINTS = 20

export function useHostUpdates(hostId: string) {
  const fetcher = useCallback((signal: AbortSignal) => getHostUpdates(hostId, HISTORY_POINTS, signal), [hostId])
  return usePolledFetch(fetcher, { intervalMs: REFRESH_MS, stopOn: isClientError })
}

export function useFleetUpdates(filters: FleetFilters, cursor: string | null) {
  // La query serializada es la identidad: `filters` se re-crea en cada render.
  const qs = buildFleetQuery(filters, cursor).toString()
  const fetcher = useCallback((signal: AbortSignal) => listFleetUpdates(new URLSearchParams(qs), signal), [qs])
  return usePolledFetch(fetcher, { intervalMs: REFRESH_MS, stopOn: isClientError })
}
