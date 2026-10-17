import {
  IconLayoutSidebarLeftCollapse,
  IconLayoutSidebarLeftExpand,
} from '@tabler/icons-react'
import { cn } from 'cn'
import { NavLink, useMatch, useResolvedPath } from 'react-router'
import { AppLogo } from '@/components/ui/app-logo'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { OpenAlertsCount } from '@/features/alerts/components/OpenAlertsCount'
import { UserMenu } from '@/features/auth/components/UserMenu'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { ThemeToggle } from '@/features/theme/components/ThemeToggle'
import { NAV_ITEMS, groupBySection, isNavItemVisible, type NavItem } from '../nav-items'

interface SidebarNavLinkProps {
  item: NavItem
  collapsed: boolean
  onNavigate?: () => void
}

function SidebarNavLink({ item, collapsed, onNavigate }: SidebarNavLinkProps) {
  const { Icon, to, label, end, openAlertsCount } = item
  // className como string (no funcion): el Slot del Tooltip de Radix concatena className y romperia una funcion.
  const isActive = useMatch({ path: useResolvedPath(to).pathname, end: end ?? false }) !== null
  // NavLink pone aria-current="page" solo en el activo. Activo = fondo + peso + barra de 2px en `text` (no `brand`).
  const link = (
    <NavLink
      to={to}
      end={end}
      onClick={onNavigate}
      aria-label={collapsed ? label : undefined}
      className={cn(
        'relative flex h-9 items-center gap-2 overflow-hidden rounded-md text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring',
        collapsed ? 'justify-center' : 'px-3',
        isActive ? 'bg-muted font-semibold text-text' : 'text-text-muted hover:bg-muted hover:text-text',
      )}
    >
      {isActive && <span aria-hidden className="absolute inset-y-0 left-0 w-0.5 bg-text" />}
      <Icon className="size-4 shrink-0" stroke={1.5} aria-hidden />
      {!collapsed && <span className="truncate">{label}</span>}
      {!collapsed && openAlertsCount && <OpenAlertsCount />}
    </NavLink>
  )

  if (!collapsed) return link
  // El tooltip de Radix tambien abre al recibir foco por teclado.
  return (
    <Tooltip>
      <TooltipTrigger asChild>{link}</TooltipTrigger>
      <TooltipContent side="right">{label}</TooltipContent>
    </Tooltip>
  )
}

interface SidebarContentProps {
  /** Solo iconos (56px). En el drawer movil siempre es false. */
  collapsed?: boolean
  /** Si se pasa, se muestra el boton de contraer/expandir (solo escritorio). */
  onToggleCollapsed?: () => void
  /** Se llama al navegar (el drawer se cierra). */
  onNavigate?: () => void
}

// Contenido unico del sidebar: lo usan el aside de escritorio y el drawer (Sheet) movil.
export function SidebarContent({ collapsed = false, onToggleCollapsed, onNavigate }: SidebarContentProps) {
  const { user } = useAuth()
  const items = NAV_ITEMS.filter((item) => isNavItemVisible(item, user))
  const CollapseIcon = collapsed ? IconLayoutSidebarLeftExpand : IconLayoutSidebarLeftCollapse
  const collapseLabel = collapsed ? 'Expandir menú' : 'Contraer menú'

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className={cn('flex h-12 shrink-0 items-center gap-2', collapsed ? 'justify-center' : 'px-4')}>
        <AppLogo withName={!collapsed} />
      </div>

      <nav aria-label="Principal" className="min-h-0 flex-1 overflow-y-auto p-2">
        {groupBySection(items).map(([section, list]) => (
          <div key={section} className="flex flex-col gap-1">
            {!collapsed && (
              <p aria-hidden className="px-3 pt-2 pb-1 text-xs text-text-muted">
                {section}
              </p>
            )}
            <ul aria-label={section} className="flex flex-col gap-1">
              {list.map((item) => (
                <li key={item.to}>
                  <SidebarNavLink item={item} collapsed={collapsed} onNavigate={onNavigate} />
                </li>
              ))}
            </ul>
          </div>
        ))}
      </nav>

      <div className={cn('flex shrink-0 flex-col gap-1 border-t border-border p-2', collapsed && 'items-center')}>
        <UserMenu collapsed={collapsed} />
        <div className={cn('flex items-center gap-1', collapsed ? 'flex-col' : 'justify-between px-1')}>
          <ThemeToggle />
          {onToggleCollapsed && (
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  onClick={onToggleCollapsed}
                  aria-label={collapseLabel}
                  aria-expanded={!collapsed}
                >
                  <CollapseIcon stroke={1.5} aria-hidden />
                </Button>
              </TooltipTrigger>
              <TooltipContent side="right">{collapseLabel}</TooltipContent>
            </Tooltip>
          )}
        </div>
      </div>
    </div>
  )
}
