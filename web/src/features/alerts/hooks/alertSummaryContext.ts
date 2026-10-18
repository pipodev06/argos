import { createContext, useContext } from 'react'
import type { usePolledFetch } from '@/hooks/usePolledFetch'
import type { AlertSummary } from '../types/alert.types'

export type AlertSummaryState = ReturnType<typeof usePolledFetch<AlertSummary>>

export const AlertSummaryContext = createContext<AlertSummaryState | null>(null)

/** Contadores de alertas activas (sidebar y resumen): una sola consulta, la del `AlertSummaryProvider` del shell. */
export function useAlertSummary(): AlertSummaryState {
  const state = useContext(AlertSummaryContext)
  if (!state) throw new Error('useAlertSummary requiere AlertSummaryProvider')
  return state
}
