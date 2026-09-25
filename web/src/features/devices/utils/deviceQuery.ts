import type { FilterField, FiltersOf } from '@/components/filters/types'
import { DEFAULT_PAGE_SIZE, PAGE_SIZES } from '@/lib/pagination'

/** La API de dispositivos solo pagina (sin busqueda ni filtros): unico parametro de URL, `limit`. */
export const DEVICE_FIELDS = [
  { kind: 'choice', key: 'limit', label: 'Tamaño de página', values: PAGE_SIZES, default: DEFAULT_PAGE_SIZE },
] as const satisfies readonly FilterField[]

export type DeviceFilters = FiltersOf<typeof DEVICE_FIELDS>

/** Query de GET /devices. */
export function buildDeviceQuery(f: DeviceFilters, cursor: string | null): string {
  const p = new URLSearchParams({ limit: String(f.limit) })
  if (cursor) p.set('cursor', cursor)
  return p.toString()
}
