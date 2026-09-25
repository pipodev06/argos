import type { FilterField, FiltersOf } from '@/components/filters/types'

/** Parámetros de URL de Informes (valores fijos, un solo valor cada uno; los controles son SegmentedControl). */
export const REPORT_FIELDS = [
  { kind: 'choice', key: 'type', label: 'Informe', values: ['availability', 'service-checks'], default: 'availability' },
  { kind: 'choice', key: 'range', label: 'Rango', values: ['7d', '30d', '90d'], default: '30d' },
  { kind: 'search', key: 'group', label: 'Grupo', ariaLabel: 'Grupo', placeholder: 'Grupo', maxLength: 36 },
  { kind: 'choice', key: 'by', label: 'Vista', values: ['host', 'group'], default: 'host' },
] as const satisfies readonly FilterField[]

export type ReportFilters = FiltersOf<typeof REPORT_FIELDS>

export const TYPE_OPTIONS = [
  { value: 'availability', label: 'Disponibilidad' },
  { value: 'service-checks', label: 'Chequeos' },
] as const

export const RANGE_OPTIONS = [
  { value: '7d', label: '7 días' },
  { value: '30d', label: '30 días' },
  { value: '90d', label: '90 días' },
] as const

export const VIEW_OPTIONS = [
  { value: 'host', label: 'Por host' },
  { value: 'group', label: 'Por grupo' },
] as const
