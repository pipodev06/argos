import { IconBell, IconBook2, IconShieldCheck, IconFolders, IconHeartRateMonitor, IconHistory, IconLayoutDashboard, IconListCheck, IconSettings, IconReportAnalytics, IconRouter, IconServer2, IconTool, IconUsers, IconWebhook, type Icon } from '@tabler/icons-react'
import type { Role, User } from '@/features/auth/types/auth.types'

export interface NavItem {
  to: string
  label: string
  Icon: Icon
  /** Titulo del grupo en el sidebar. */
  section: string
  /** Roles que ven el item; sin definir = todos. */
  roles?: readonly Role[]
  /** Solo para quien ve todos los grupos (los viewers restringidos no lo ven). */
  fullScope?: boolean
  /** Solo admin o quien tenga el permiso `can_updates` (parches del SO). */
  requiresUpdates?: boolean
  /** `end` de NavLink: solo activo en la ruta exacta (necesario para `/`). */
  end?: boolean
  /** Contador de alertas abiertas junto a la etiqueta. */
  openAlertsCount?: boolean
}

// Solo hay items para rutas que existen (sin "proximamente").
export const NAV_ITEMS: readonly NavItem[] = [
  { to: '/', label: 'Resumen', Icon: IconLayoutDashboard, section: 'Monitoreo', end: true },
  { to: '/hosts', label: 'Hosts', Icon: IconServer2, section: 'Monitoreo' },
  { to: '/dispositivos', label: 'Dispositivos', Icon: IconRouter, section: 'Monitoreo' },
  { to: '/grupos', label: 'Grupos', Icon: IconFolders, section: 'Monitoreo' },
  { to: '/chequeos', label: 'Chequeos', Icon: IconHeartRateMonitor, section: 'Monitoreo' },
  { to: '/parches', label: 'Parches', Icon: IconShieldCheck, section: 'Monitoreo', requiresUpdates: true },
  { to: '/informes', label: 'Informes', Icon: IconReportAnalytics, section: 'Monitoreo' },
  { to: '/alertas', label: 'Alertas', Icon: IconBell, section: 'Alertas', end: true, openAlertsCount: true },
  { to: '/alertas/reglas', label: 'Reglas', Icon: IconListCheck, section: 'Alertas' },
  { to: '/alertas/mantenimiento', label: 'Mantenimiento', Icon: IconTool, section: 'Alertas' },
  { to: '/alertas/canales', label: 'Canales', Icon: IconWebhook, section: 'Alertas', fullScope: true },
  { to: '/usuarios', label: 'Usuarios', Icon: IconUsers, section: 'Administración', roles: ['admin'] },
  { to: '/configuracion', label: 'Configuración', Icon: IconSettings, section: 'Administración', roles: ['admin'] },
  { to: '/auditoria', label: 'Auditoría', Icon: IconHistory, section: 'Administración', roles: ['admin'] },
  { to: '/instrucciones', label: 'Instrucciones', Icon: IconBook2, section: 'Administración', roles: ['admin'] },
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

/** Visibilidad de un item segun el usuario (el servidor vuelve a validar cada ruta). */
export function isNavItemVisible(item: NavItem, user: Pick<User, 'role' | 'all_groups' | 'can_updates'> | null): boolean {
  if (item.roles && !(user !== null && item.roles.includes(user.role))) return false
  if (item.fullScope && user?.all_groups !== true) return false
  if (item.requiresUpdates && !(user !== null && (user.role === 'admin' || user.can_updates))) return false
  return true
}
