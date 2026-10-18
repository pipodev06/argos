import { useCallback } from 'react'
import { isClientError, usePolledFetch } from '@/hooks/usePolledFetch'
import { getAlert, listAlerts, listHostEventsPage, listMaintenance, listRules } from '../services/alerts.service'
import { buildAlertQuery, type AlertFilters } from '../utils/alertQuery'

const REFRESH_MS = 30_000
const listOptions = { intervalMs: REFRESH_MS, stopOn: isClientError }
const detailOptions = { intervalMs: REFRESH_MS, stopOn: isClientError }

export function useAlertList(filters: AlertFilters, cursor: string | null) {
  // La query serializada es la identidad: `filters` se re-crea en cada render.
  const qs = buildAlertQuery(filters, cursor).toString()
  const fetcher = useCallback((signal: AbortSignal) => listAlerts(new URLSearchParams(qs), signal), [qs])
  return usePolledFetch(fetcher, listOptions)
}

export function useAlertDetail(id: string) {
  const fetcher = useCallback((signal: AbortSignal) => getAlert(id, signal), [id])
  return usePolledFetch(fetcher, detailOptions)
}

export function useAlertRules() {
  return usePolledFetch(listRules, listOptions)
}

export function useMaintenanceList() {
  return usePolledFetch(listMaintenance, listOptions)
}

export function useHostEventsPage(hostId: string, limit: number, cursor: string | null) {
  const fetcher = useCallback((signal: AbortSignal) => listHostEventsPage(hostId, limit, cursor, signal), [hostId, limit, cursor])
  return usePolledFetch(fetcher, { intervalMs: REFRESH_MS, stopOn: isClientError })
}
