import { useCallback } from 'react'
import { isClientError, usePolledFetch } from '@/hooks/usePolledFetch'
import { getHostHistory } from '../services/hosts.service'
import type { DateRange } from '../utils/history'

const UNUSED_INTERVAL_MS = 24 * 60 * 60 * 1000

/** Consulta unica del historial de un rango fijo (sin refrescos: los datos viejos no cambian). */
export function useHostHistory(id: string, { fromMs, toMs }: DateRange) {
  const fetcher = useCallback(
    (signal: AbortSignal) => getHostHistory(id, { fromMs, toMs }, signal),
    [id, fromMs, toMs],
  )
  return usePolledFetch(fetcher, { intervalMs: UNUSED_INTERVAL_MS, stopOn: isClientError, paused: true })
}
