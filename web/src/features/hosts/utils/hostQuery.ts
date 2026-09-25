import type { FilterField, FiltersOf, NumericUnit } from '@/components/filters/types'
import { serializeFilters } from '@/components/filters/urlState'
import { DEFAULT_PAGE_SIZE, PAGE_SIZES } from '@/lib/pagination'
import { useGroupOptions } from '../hooks/useGroupOptions'
import type { HostStatus } from '../types/host.types'
import { OS_LABELS } from './os'
import { HOST_STATUS_LABELS } from './status'

export const SORT_KEYS = ['severity', 'name', 'status', 'group', 'cpu', 'mem', 'disk', 'last_seen'] as const
export type SortKey = (typeof SORT_KEYS)[number]

export const DEFAULT_SORT: SortKey = 'severity'
const HOST_STATUSES: readonly HostStatus[] = ['up', 'down', 'offline', 'pending', 'disabled']
const OS_VALUES = ['windows', 'linux', 'darwin'] as const

const DESC_BY_DEFAULT: readonly SortKey[] = ['cpu', 'mem', 'disk', 'last_seen']

export function defaultDir(sort: SortKey): 'asc' | 'desc' {
  return DESC_BY_DEFAULT.includes(sort) ? 'desc' : 'asc'
}

/** Ordenar por una metrica volatil reordena filas a cada muestra: se congela el refresco automatico. */
export function isVolatileSort(sort: SortKey): boolean {
  return sort === 'cpu' || sort === 'mem' || sort === 'disk'
}

const SEEN_UNITS: readonly NumericUnit[] = [
  { id: 'min', label: 'Minutos', one: 'minuto', many: 'minutos', factor: 60 },
  { id: 'h', label: 'Horas', one: 'hora', many: 'horas', factor: 3600 },
  { id: 'd', label: 'Días', one: 'día', many: 'días', factor: 86400 },
]

/**
 * Campos de filtro de Hosts (orden = orden de la fila y de la URL). Filtros, orden y tamano de pagina
 * viven en la URL (el cursor no). Nombres de parametro: status, group, os, cpu_gt, mem_gt, disk_gt,
 * battery_lt, seen_before, q, sort, dir, limit.
 */
export const HOST_FIELDS = [
  { kind: 'search', key: 'q', label: 'Buscar', ariaLabel: 'Buscar hosts', placeholder: 'Buscar (/)' },
  {
    kind: 'multi',
    key: 'status',
    label: 'Estado',
    plural: 'estados',
    options: HOST_STATUSES.map((s) => ({ id: s, name: HOST_STATUS_LABELS[s] })),
  },
  /** Ids de grupo o "none" (sin grupo). */
  { kind: 'multi', key: 'group', label: 'Grupo', plural: 'grupos', useOptions: useGroupOptions },
  {
    kind: 'multi',
    key: 'os',
    label: 'SO',
    plural: 'sistemas',
    options: OS_VALUES.map((o) => ({ id: o, name: OS_LABELS[o] })),
  },
  { kind: 'numeric', key: 'cpu_gt', label: 'CPU', extra: true, op: '>', max: 100, suffix: '%' },
  { kind: 'numeric', key: 'mem_gt', label: 'Memoria', extra: true, op: '>', max: 100, suffix: '%' },
  { kind: 'numeric', key: 'disk_gt', label: 'Disco', extra: true, op: '>', max: 100, suffix: '%' },
  { kind: 'numeric', key: 'battery_lt', label: 'Batería', extra: true, op: '<', max: 100, suffix: '%' },
  {
    kind: 'numeric',
    key: 'seen_before',
    label: 'Visto hace',
    extra: true,
    op: '>',
    integer: true,
    units: SEEN_UNITS,
    defaultUnit: 'h',
    baseUnit: 's',
  },
  { kind: 'choice', key: 'sort', label: 'Orden', values: SORT_KEYS, default: DEFAULT_SORT },
  {
    kind: 'choice',
    key: 'dir',
    label: 'Dirección',
    values: ['asc', 'desc'],
    // `sort` ya fue leido (va antes en la lista): la direccion por defecto depende de el.
    default: (v) => defaultDir(v.sort as SortKey),
  },
  { kind: 'choice', key: 'limit', label: 'Tamaño de página', values: PAGE_SIZES, default: DEFAULT_PAGE_SIZE },
] as const satisfies readonly FilterField[]

export type HostFilters = FiltersOf<typeof HOST_FIELDS>

/** Query de GET /hosts: mismos filtros, con orden/direccion explicitos, limite y cursor. */
export function buildApiQuery(f: HostFilters, cursor: string | null): URLSearchParams {
  const p = serializeFilters(HOST_FIELDS, f)
  p.set('sort', f.sort)
  p.set('dir', f.dir)
  p.set('limit', String(f.limit))
  if (cursor) p.set('cursor', cursor)
  return p
}
