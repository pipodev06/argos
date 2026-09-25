import { useCallback } from 'react'
import { isClientError, usePolledFetch } from '@/hooks/usePolledFetch'
import { getAvailabilityReport, getServiceChecksReport } from '../services/reports.service'
import type { ReportRange, ReportView } from '../types/report.types'

const REFRESH_MS = 30_000

export function useAvailabilityReport(range: ReportRange = '30d', by: ReportView = 'host', groupId?: string) {
  const fetcher = useCallback((signal: AbortSignal) => getAvailabilityReport(range, by, groupId, signal), [range, by, groupId])
  return usePolledFetch(fetcher, { intervalMs: REFRESH_MS, stopOn: isClientError })
}

export function useServiceChecksReport(range: ReportRange = '30d', groupId?: string) {
  const fetcher = useCallback((signal: AbortSignal) => getServiceChecksReport(range, groupId, signal), [range, groupId])
  return usePolledFetch(fetcher, { intervalMs: REFRESH_MS, stopOn: isClientError })
}
