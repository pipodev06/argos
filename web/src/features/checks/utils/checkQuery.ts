import type { FilterField, FiltersOf } from '@/components/filters/types'
import type { CheckType, ServiceCheck } from '../types/check.types'
import { CHECK_TYPE_LABELS } from './format'

const TYPES = Object.keys(CHECK_TYPE_LABELS) as CheckType[]

/** Campos de filtro de Chequeos (parámetros de URL: q, type, enabled). La API no filtra: se filtra en el cliente. */
export const CHECK_FIELDS = [
  { kind: 'search', key: 'q', label: 'Nombre', ariaLabel: 'Buscar chequeo por nombre', placeholder: 'Buscar nombre', maxLength: 80 },
  { kind: 'multi', key: 'type', label: 'Tipo', plural: 'tipos', options: TYPES.map((t) => ({ id: t, name: CHECK_TYPE_LABELS[t] })) },
  {
    kind: 'multi',
    key: 'enabled',
    label: 'Activo',
    plural: 'estados',
    options: [
      { id: 'yes', name: 'Sí' },
      { id: 'no', name: 'No' },
    ],
  },
  {
    kind: 'multi',
    key: 'health',
    label: 'Estado',
    plural: 'estados',
    options: [
      { id: 'failing', name: 'Con falla' },
      { id: 'ok', name: 'Sin falla' },
    ],
  },
] as const satisfies readonly FilterField[]

export type CheckFilters = FiltersOf<typeof CHECK_FIELDS>

export function filterChecks(items: readonly ServiceCheck[], f: CheckFilters): ServiceCheck[] {
  const q = f.q.trim().toLowerCase()
  return items.filter(
    (c) =>
      (q === '' || c.name.toLowerCase().includes(q)) &&
      (f.type.length === 0 || f.type.includes(c.type)) &&
      (f.enabled.length === 0 || f.enabled.includes(c.enabled ? 'yes' : 'no')) &&
      (f.health.length === 0 || f.health.includes((c.failing_hosts ?? 0) > 0 ? 'failing' : 'ok')),
  )
}
