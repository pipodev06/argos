import type { FilterField, FiltersOf } from '@/components/filters/types'
import { serializeFilters } from '@/components/filters/urlState'
import { DEFAULT_PAGE_SIZE, PAGE_SIZES } from '@/lib/pagination'
import { useAuditEventOptions } from '../hooks/useAuditEventOptions'
import type { AuditOutcome } from '../types/audit.types'
import { OUTCOME_LABELS } from './labels'

const OUTCOMES: readonly AuditOutcome[] = ['ok', 'fail', 'denied']

/** Mensaje cuando el servidor rechaza los filtros (400 invalid_filter). */
export const FILTER_ERROR =
  'Filtros no válidos: "Desde" debe ser anterior a "Hasta" y el rango no puede superar 366 días.'

/** Campos de filtro de Auditoría. Parámetros de URL/API: actor, event, outcome, from, to, limit. */
export const AUDIT_FIELDS = [
  { kind: 'search', key: 'actor', label: 'Actor', ariaLabel: 'Buscar por actor', placeholder: 'Buscar actor', maxLength: 64 },
  { kind: 'multi', key: 'event', label: 'Evento', plural: 'eventos', useOptions: useAuditEventOptions, searchAbove: 8 },
  {
    kind: 'multi',
    key: 'outcome',
    label: 'Resultado',
    plural: 'resultados',
    options: OUTCOMES.map((o) => ({ id: o, name: OUTCOME_LABELS[o] })),
  },
  { kind: 'datetime', key: 'from', label: 'Desde' },
  { kind: 'datetime', key: 'to', label: 'Hasta' },
  { kind: 'choice', key: 'limit', label: 'Tamaño de página', values: PAGE_SIZES, default: DEFAULT_PAGE_SIZE },
] as const satisfies readonly FilterField[]

export type AuditFilters = FiltersOf<typeof AUDIT_FIELDS>

export function buildAuditQuery(f: AuditFilters, cursor: string | null): URLSearchParams {
  const p = serializeFilters(AUDIT_FIELDS, f)
  p.set('limit', String(f.limit))
  if (cursor) p.set('cursor', cursor)
  return p
}
