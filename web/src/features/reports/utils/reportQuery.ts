import type { FilterField, FiltersOf } from '@/components/filters/types'

/** Campos de filtro de Informes (parámetros de URL: type, range, by, group_id). */
export const REPORT_FIELDS = [
  {
    kind: 'multi',
    key: 'type',
    label: 'Informe',
    plural: 'informes',
    options: [
      { id: 'availability', name: 'Disponibilidad' },
      { id: 'service-checks', name: 'Chequeos' },
    ],
  },
  {
    kind: 'multi',
    key: 'range',
    label: 'Rango',
    plural: 'rangos',
    options: [
      { id: '7d', name: '7 días' },
      { id: '30d', name: '30 días' },
      { id: '90d', name: '90 días' },
    ],
  },
  {
    kind: 'multi',
    key: 'by',
    label: 'Vista',
    plural: 'vistas',
    options: [
      { id: 'host', name: 'Por host' },
      { id: 'group', name: 'Por grupo' },
    ],
  },
] as const satisfies readonly FilterField[]

export type ReportFilters = FiltersOf<typeof REPORT_FIELDS>
