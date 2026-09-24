import { useCallback } from 'react'
import { usePolledFetch } from '@/hooks/usePolledFetch'
import { listAudit } from '../services/audit.service'
import { buildAuditQuery, type AuditFilters } from '../utils/auditQuery'

// Sin polling automatico: solo carga inicial y "Actualizar".
const options = { intervalMs: 60_000, paused: true }

export function useAuditList(filters: AuditFilters, cursor: string | null) {
  // La query serializada es la identidad: `filters` se re-crea en cada render.
  const qs = buildAuditQuery(filters, cursor).toString()
  const fetcher = useCallback((signal: AbortSignal) => listAudit(new URLSearchParams(qs), signal), [qs])
  return usePolledFetch(fetcher, options)
}
