import { IconBell, IconFolders, IconHeartRateMonitor, IconHistory, IconLayoutDashboard, IconListCheck, IconServer2, IconTool, IconUsers, IconWebhook, type Icon } from '@tabler/icons-react'
import type { Role } from '@/features/auth/types/auth.types'

export interface NavItem {
  to: string
  label: string
  Icon: Icon
  /** Titulo del grupo en el sidebar. */
  section: string
  /** Roles que ven el item; sin definir = todos. */
  roles?: readonly Role[]
  /** `end` de NavLink: solo activo en la ruta exacta (necesario para `/`). */
  end?: boolean
  /** Contador de alertas abiertas junto a la etiqueta. */
  openAlertsCount?: boolean
}

// Solo hay items para rutas que existen (sin "proximamente").
export const NAV_ITEMS: readonly NavItem[] = [
  { to: '/', label: 'Resumen', Icon: IconLayoutDashboard, section: 'Monitoreo', end: true },
  { to: '/hosts', label: 'Hosts', Icon: IconServer2, section: 'Monitoreo' },
  { to: '/grupos', label: 'Grupos', Icon: IconFolders, section: 'Monitoreo' },
  { to: '/chequeos', label: 'Chequeos', Icon: IconHeartRateMonitor, section: 'Monitoreo' },
  { to: '/alertas', label: 'Alertas', Icon: IconBell, section: 'Alertas', end: true, openAlertsCount: true },
  { to: '/alertas/reglas', label: 'Reglas', Icon: IconListCheck, section: 'Alertas' },
  { to: '/alertas/mantenimiento', label: 'Mantenimiento', Icon: IconTool, section: 'Alertas' },
  { to: '/alertas/canales', label: 'Canales', Icon: IconWebhook, section: 'Alertas' },
  { to: '/usuarios', label: 'Usuarios', Icon: IconUsers, section: 'Administración', roles: ['admin'] },
  { to: '/auditoria', label: 'Auditoría', Icon: IconHistory, section: 'Administración', roles: ['admin'] },
]

/** Agrupa por `section` conservando el orden de aparicion. */
export function groupBySection(items: readonly NavItem[]): [string, NavItem[]][] {
  const groups = new Map<string, NavItem[]>()
  for (const item of items) {
    const list = groups.get(item.section)
    if (list) list.push(item)
    else groups.set(item.section, [item])
  }
  return [...groups]
}
