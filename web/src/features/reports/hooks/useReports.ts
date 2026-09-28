import { useCallback } from 'react'
import { isClientError, usePolledFetch } from '@/hooks/usePolledFetch'
import { getAvailabilityReport, getServiceChecksReport } from '../services/reports.service'
import type { ReportRange, ReportView } from '../types/report.types'

// Informes de 7 a 90 días: 5 min basta (y el servidor limita a 2 informes simultáneos).
const REFRESH_MS = 300_000

// from/to (B12, rango libre) van sueltos: son primitivas, a diferencia de un objeto `{from,to}` que seria
// una referencia nueva cada render y dispararia el fetch en bucle.
export function useAvailabilityReport(range: ReportRange = '30d', by: ReportView = 'host', groupId?: string, from?: string, to?: string) {
  const fetcher = useCallback(
    (signal: AbortSignal) => getAvailabilityReport(range, by, groupId, from, to, signal),
    [range, by, groupId, from, to],
  )
  return usePolledFetch(fetcher, { intervalMs: REFRESH_MS, stopOn: isClientError })
}

export function useServiceChecksReport(range: ReportRange = '30d', groupId?: string, from?: string, to?: string) {
  const fetcher = useCallback((signal: AbortSignal) => getServiceChecksReport(range, groupId, from, to, signal), [range, groupId, from, to])
  return usePolledFetch(fetcher, { intervalMs: REFRESH_MS, stopOn: isClientError })
}
