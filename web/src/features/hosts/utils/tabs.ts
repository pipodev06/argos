import type { User } from '@/features/auth/types/auth.types'
import { canViewInventory } from '@/features/inventory/utils/inventory'
import { canViewUpdates } from '@/features/updates/utils/updates'

export type HostTabId = 'resumen' | 'metricas' | 'chequeos' | 'dispositivos' | 'parches' | 'inventario' | 'historial'

interface HostTab {
  id: HostTabId
  label: string
  /** Segmento de la URL bajo `/hosts/:id` ('' = el Resumen). */
  path: string
}

const TABS: readonly HostTab[] = [
  { id: 'resumen', label: 'Resumen', path: '' },
  { id: 'metricas', label: 'Métricas', path: 'metricas' },
  { id: 'chequeos', label: 'Chequeos', path: 'chequeos' },
  { id: 'dispositivos', label: 'Dispositivos', path: 'dispositivos' },
  { id: 'parches', label: 'Parches', path: 'parches' },
  { id: 'inventario', label: 'Inventario', path: 'inventario' },
  { id: 'historial', label: 'Historial de estado', path: 'historial' },
]

/** Pestañas que ve el usuario: Dispositivos solo en colectores; Parches e Inventario según permiso. */
export function visibleHostTabs(
  user: Pick<User, 'role' | 'can_updates' | 'can_inventory'> | null,
  isCollector: boolean,
): readonly HostTab[] {
  return TABS.filter((t) => {
    if (t.id === 'dispositivos') return isCollector
    if (t.id === 'parches') return canViewUpdates(user)
    if (t.id === 'inventario') return canViewInventory(user)
    return true
  })
}

export function hostTabPath(hostId: string, path: string): string {
  const base = `/hosts/${encodeURIComponent(hostId)}`
  return path ? `${base}/${path}` : base
}

/** URL vieja `?tab=x` a su sub-ruta; null si el valor no es una pestaña. */
export function legacyTabPath(hostId: string, tab: string | null): string | null {
  const found = TABS.find((t) => t.id === tab)
  return found ? hostTabPath(hostId, found.path) : null
}

/** Pestaña de una URL (por su último segmento bajo el host); Resumen si no coincide. */
export function tabForPath(hostId: string, pathname: string): HostTab {
  const base = `/hosts/${encodeURIComponent(hostId)}/`
  const rest = pathname.startsWith(base) ? pathname.slice(base.length).replace(/\/$/, '') : ''
  return TABS.find((t) => t.path === rest) ?? TABS[0]
}
