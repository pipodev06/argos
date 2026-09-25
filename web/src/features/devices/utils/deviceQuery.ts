import type { FilterField, FiltersOf } from '@/components/filters/types'
import { serializeFilters } from '@/components/filters/urlState'
import { useGroupOptions } from '@/features/hosts/hooks/useGroupOptions'
import { HOST_STATUS_LABELS } from '@/features/hosts/utils/status'
import type { HostStatus } from '@/features/hosts/types/host.types'
import { DEFAULT_PAGE_SIZE, PAGE_SIZES } from '@/lib/pagination'

const DEVICE_STATUSES: readonly HostStatus[] = ['up', 'down', 'offline', 'pending', 'disabled']

/** Filtros de Dispositivos (estado en la URL, igual que Hosts): q, status, group y limit. */
export const DEVICE_FIELDS = [
  { kind: 'search', key: 'q', label: 'Buscar', ariaLabel: 'Buscar dispositivos', placeholder: 'Buscar' },
  {
    kind: 'multi',
    key: 'status',
    label: 'Estado',
    plural: 'estados',
    options: DEVICE_STATUSES.map((s) => ({ id: s, name: HOST_STATUS_LABELS[s] })),
  },
  /** Ids de grupo o "none" (sin grupo). */
  { kind: 'multi', key: 'group', label: 'Grupo', plural: 'grupos', useOptions: useGroupOptions },
  { kind: 'choice', key: 'limit', label: 'Tamaño de página', values: PAGE_SIZES, default: DEFAULT_PAGE_SIZE },
] as const satisfies readonly FilterField[]

export type DeviceFilters = FiltersOf<typeof DEVICE_FIELDS>

/** Query de GET /devices: filtros, limite y cursor. */
export function buildDeviceQuery(f: DeviceFilters, cursor: string | null): string {
  const p = serializeFilters(DEVICE_FIELDS, f)
  p.set('limit', String(f.limit))
  if (cursor) p.set('cursor', cursor)
  return p.toString()
}
