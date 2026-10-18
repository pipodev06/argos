import type { ReactNode } from 'react'
import { isClientError, usePolledFetch } from '@/hooks/usePolledFetch'
import { AlertSummaryContext } from '../hooks/alertSummaryContext'
import { getAlertSummary } from '../services/alerts.service'

const OPTIONS = { intervalMs: 30_000, stopOn: isClientError }

/** Consulta una sola vez los contadores de alertas para el sidebar y el bloque del resumen. */
export function AlertSummaryProvider({ children }: { children: ReactNode }) {
  const state = usePolledFetch(getAlertSummary, OPTIONS)
  return <AlertSummaryContext.Provider value={state}>{children}</AlertSummaryContext.Provider>
}
