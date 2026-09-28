import type { FilterField, FiltersOf } from '@/components/filters/types'
import { serializeFilters } from '@/components/filters/urlState'
import { useGroupOptions } from '@/features/hosts/hooks/useGroupOptions'
import { OS_LABELS } from '@/features/hosts/utils/os'
import { DEFAULT_PAGE_SIZE, PAGE_SIZES } from '@/lib/pagination'

const SORT_KEYS = ['security', 'pending', 'reboot', 'eol', 'name', 'updated'] as const
export type SortKey = (typeof SORT_KEYS)[number]
const DEFAULT_SORT: SortKey = 'security'

/** Por defecto descendente lo que "mas es peor" (seguridad, pendientes, reinicio); el resto ascendente. */
export function defaultDir(sort: SortKey): 'asc' | 'desc' {
  return sort === 'security' || sort === 'pending' || sort === 'reboot' ? 'desc' : 'asc'
}

const OS_VALUES = ['windows', 'linux', 'darwin'] as const
/** Filtros de la flota de parches (URL: q, group, os, security, reboot, eol_days, sort, dir, limit; los chips booleanos se combinan con AND). */
export const FLEET_FIELDS = [
  { kind: 'search', key: 'q', label: 'Buscar', ariaLabel: 'Buscar hosts', placeholder: 'Buscar' },
  { kind: 'multi', key: 'group', label: 'Grupo', plural: 'grupos', useOptions: useGroupOptions, contentClassName: 'w-64' },
  { kind: 'multi', key: 'os', label: 'SO', plural: 'sistemas', options: OS_VALUES.map((o) => ({ id: o, name: OS_LABELS[o] })) },
  { kind: 'boolean', key: 'security', label: 'Con seguridad pendiente', extra: true },
  { kind: 'boolean', key: 'reboot', label: 'Reinicio pendiente', extra: true },
  { kind: 'numeric', key: 'eol_days', label: 'Días al fin de vida', extra: true, op: '≤', max: 3650, integer: true, suffix: 'd' },
  { kind: 'choice', key: 'sort', label: 'Orden', values: SORT_KEYS, default: DEFAULT_SORT },
  { kind: 'choice', key: 'dir', label: 'Dirección', values: ['asc', 'desc'], default: (v) => defaultDir(v.sort as SortKey) },
  { kind: 'choice', key: 'limit', label: 'Tamaño de página', values: PAGE_SIZES, default: DEFAULT_PAGE_SIZE },
] as const satisfies readonly FilterField[]

export type FleetFilters = FiltersOf<typeof FLEET_FIELDS>

/** Query de GET /updates (`security=true`, `reboot=true`, `eol_days=N` con semantica ≤); orden y limite explicitos. */
export function buildFleetQuery(f: FleetFilters, cursor: string | null): URLSearchParams {
  const p = serializeFilters(FLEET_FIELDS, f)
  p.set('sort', f.sort)
  p.set('dir', f.dir)
  p.set('limit', String(f.limit))
  if (cursor) p.set('cursor', cursor)
  return p
}

/** Query de GET /updates.csv: mismos filtros que la lista, sin paginacion/orden. */
export function buildFleetCsvQuery(f: FleetFilters): URLSearchParams {
  return serializeFilters(FLEET_FIELDS, f)
}
