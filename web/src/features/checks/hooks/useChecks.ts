import { useCallback } from 'react'
import { isClientError, usePolledFetch } from '@/hooks/usePolledFetch'
import { listChecks, listHostChecks } from '../services/checks.service'

const REFRESH_MS = 30_000

export function useServiceChecks() {
  return usePolledFetch(listChecks, { intervalMs: REFRESH_MS })
}

/** Chequeos de un host: refresco 30 s, pausa con la pestaña oculta y se detiene ante 401 o error de cliente. */
export function useHostChecks(hostId: string) {
  const fetcher = useCallback((signal: AbortSignal) => listHostChecks(hostId, signal), [hostId])
  return usePolledFetch(fetcher, { intervalMs: REFRESH_MS, stopOn: isClientError })
}
