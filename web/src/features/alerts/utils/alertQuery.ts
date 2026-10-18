import type { FilterField, FiltersOf } from '@/components/filters/types'
import { serializeFilters } from '@/components/filters/urlState'
import { DEFAULT_PAGE_SIZE, PAGE_SIZES } from '@/lib/pagination'
import { useRuleOptions } from '../hooks/useRuleOptions'
import type { AlertSeverity, AlertState } from '../types/alert.types'
import { SEVERITY_LABELS, STATE_LABELS } from './format'

const STATES: readonly AlertState[] = ['open', 'acknowledged', 'resolved']
const SEVERITIES: readonly AlertSeverity[] = ['info', 'warn', 'critical']

/** Campos de filtro de Alertas. Parámetros de URL/API: state, severity, rule_id, limit. */
export const ALERT_FIELDS = [
  { kind: 'search', key: 'q', label: 'Buscar', ariaLabel: 'Buscar alertas por host', placeholder: 'Buscar host', maxLength: 128 },
  {
    kind: 'multi',
    key: 'state',
    label: 'Estado',
    plural: 'estados',
    options: STATES.map((s) => ({ id: s, name: STATE_LABELS[s] })),
  },
  {
    kind: 'multi',
    key: 'severity',
    label: 'Severidad',
    plural: 'severidades',
    options: SEVERITIES.map((s) => ({ id: s, name: SEVERITY_LABELS[s] })),
  },
  { kind: 'multi', key: 'rules', param: 'rule_id', label: 'Regla', plural: 'reglas', useOptions: useRuleOptions },
  { kind: 'datetime', key: 'from', label: 'Apertura', rangeEnd: 'to' },
  { kind: 'datetime', key: 'to', label: 'Hasta' },
  { kind: 'choice', key: 'limit', label: 'Tamaño de página', values: PAGE_SIZES, default: DEFAULT_PAGE_SIZE },
] as const satisfies readonly FilterField[]

export type AlertFilters = FiltersOf<typeof ALERT_FIELDS>

export function buildAlertQuery(f: AlertFilters, cursor: string | null): URLSearchParams {
  const p = serializeFilters(ALERT_FIELDS, f)
  p.set('limit', String(f.limit))
  if (cursor) p.set('cursor', cursor)
  return p
}
