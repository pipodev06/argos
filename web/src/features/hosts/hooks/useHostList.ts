import { useCallback } from 'react'
import { isClientError, usePolledFetch } from '@/hooks/usePolledFetch'
import { listHosts } from '../services/hosts.service'
import { buildApiQuery, type HostFilters } from '../utils/hostQuery'

const REFRESH_MS = 30_000

export function useHostList(filters: HostFilters, cursor: string | null) {
  // La query serializada es la identidad: `filters` se re-crea en cada render.
  const qs = buildApiQuery(filters, cursor).toString()
  const fetcher = useCallback((signal: AbortSignal) => listHosts(new URLSearchParams(qs), signal), [qs])
  return usePolledFetch(fetcher, { intervalMs: REFRESH_MS, stopOn: isClientError })
}
