import { BellIcon, KeyRoundIcon, ShieldCheckIcon, FoldersIcon, ActivityIcon, HistoryIcon, LayoutDashboardIcon, ListChecksIcon, SettingsIcon, FileChartColumnIcon, RouterIcon, ServerIcon, WrenchIcon, UsersIcon, WebhookIcon, type LucideIcon as Icon } from 'lucide-react'
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
  { to: '/', label: 'Resumen', Icon: LayoutDashboardIcon, section: 'Monitoreo', end: true },
  { to: '/alertas', label: 'Alertas', Icon: BellIcon, section: 'Monitoreo', end: true, openAlertsCount: true },
  { to: '/hosts', label: 'Hosts', Icon: ServerIcon, section: 'Monitoreo' },
  { to: '/dispositivos', label: 'Dispositivos', Icon: RouterIcon, section: 'Monitoreo' },
  { to: '/parches', label: 'Parches', Icon: ShieldCheckIcon, section: 'Monitoreo', requiresUpdates: true },
  { to: '/informes', label: 'Informes', Icon: FileChartColumnIcon, section: 'Monitoreo' },
  { to: '/grupos', label: 'Grupos', Icon: FoldersIcon, section: 'Configuración' },
  { to: '/chequeos', label: 'Chequeos', Icon: ActivityIcon, section: 'Configuración' },
  { to: '/alertas/reglas', label: 'Reglas de alerta', Icon: ListChecksIcon, section: 'Configuración' },
  { to: '/alertas/mantenimiento', label: 'Mantenimiento', Icon: WrenchIcon, section: 'Configuración' },
  { to: '/alertas/canales', label: 'Canales', Icon: WebhookIcon, section: 'Configuración', fullScope: true },
  { to: '/perfiles-snmp', label: 'Perfiles SNMP', Icon: KeyRoundIcon, section: 'Configuración', roles: ['admin'] },
  { to: '/usuarios', label: 'Usuarios', Icon: UsersIcon, section: 'Administración', roles: ['admin'] },
  { to: '/sistema', label: 'Sistema', Icon: SettingsIcon, section: 'Administración', roles: ['admin'] },
  { to: '/auditoria', label: 'Auditoría', Icon: HistoryIcon, section: 'Administración', roles: ['admin'] },
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

/** Item del menu que corresponde a la ruta actual (el prefijo mas largo; `/` solo exacto), para las migas de pan. */
export function navItemForPath(pathname: string): NavItem | undefined {
  let best: NavItem | undefined
  for (const item of NAV_ITEMS) {
    const hit = item.to === '/' ? pathname === '/' : pathname === item.to || pathname.startsWith(`${item.to}/`)
    if (hit && (!best || item.to.length > best.to.length)) best = item
  }
  return best
}
