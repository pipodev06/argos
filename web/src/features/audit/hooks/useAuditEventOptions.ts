import { useMemo } from 'react'
import type { FilterOption } from '@/components/filters/types'
import { usePolledFetch } from '@/hooks/usePolledFetch'
import { listAuditEvents } from '../services/audit.service'
import { eventLabel } from '../utils/labels'

// Sin polling automatico: se carga una vez al montar.
const options = { intervalMs: 60_000, paused: true }

/** Eventos existentes en el registro, como opciones del filtro (nombre legible, orden alfabetico). */
export function useAuditEventOptions(): readonly FilterOption[] {
  const { data } = usePolledFetch(listAuditEvents, options)
  return useMemo(
    () =>
      (data?.events ?? [])
        .map((id) => ({ id, name: eventLabel(id) }))
        .sort((a, b) => a.name.localeCompare(b.name, 'es')),
    [data],
  )
}
