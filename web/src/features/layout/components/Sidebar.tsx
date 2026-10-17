import { useMatch, useResolvedPath, NavLink } from 'react-router'
import { AppLogo } from '@/components/ui/app-logo'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
  SidebarTrigger,
} from '@/components/ui/sidebar'
import { useSidebar } from '@/components/ui/sidebar-context'
import { OpenAlertsCount } from '@/features/alerts/components/OpenAlertsCount'
import { UserMenu } from '@/features/auth/components/UserMenu'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { ThemeToggle } from '@/features/theme/components/ThemeToggle'
import { NAV_ITEMS, groupBySection, isNavItemVisible, type NavItem } from '../nav-items'

function NavItemLink({ item }: { item: NavItem }) {
  const { Icon, to, label, end, openAlertsCount } = item
  const { isMobile, setOpenMobile, state } = useSidebar()
  const isActive = useMatch({ path: useResolvedPath(to).pathname, end: end ?? false }) !== null
  const collapsed = state === 'collapsed' && !isMobile
  // Activo = fondo muted + semibold + barra de 2px en `text` (AGENTS.md); nunca brand.
  return (
    <SidebarMenuItem>
      <SidebarMenuButton
        asChild
        isActive={isActive}
        tooltip={label}
        className="relative text-text-muted hover:text-text data-active:font-semibold data-active:text-text"
      >
        <NavLink to={to} end={end} onClick={() => isMobile && setOpenMobile(false)}>
          {isActive && <span aria-hidden className="absolute inset-y-0 left-0 w-0.5 rounded-full bg-text" />}
          <Icon strokeWidth={1.5} aria-hidden />
          <span>{label}</span>
          {!collapsed && openAlertsCount && <OpenAlertsCount />}
        </NavLink>
      </SidebarMenuButton>
    </SidebarMenuItem>
  )
}

/** Sidebar unico (shadcn `Sidebar`, modo icono a 56px; en movil lo abre el propio componente como drawer). */
export function AppSidebar() {
  const { user } = useAuth()
  const { state, isMobile } = useSidebar()
  const collapsed = state === 'collapsed' && !isMobile
  const items = NAV_ITEMS.filter((item) => isNavItemVisible(item, user))

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader className="h-12 justify-center">
        <AppLogo withName={!collapsed} className={collapsed ? 'mx-auto' : 'px-1'} />
      </SidebarHeader>

      <SidebarContent>
        <nav aria-label="Principal">
          {groupBySection(items).map(([section, list]) => (
            <SidebarGroup key={section} className="group-data-[collapsible=icon]:px-3">
              <SidebarGroupLabel>{section}</SidebarGroupLabel>
              <SidebarGroupContent>
                <SidebarMenu aria-label={section}>
                  {list.map((item) => (
                    <NavItemLink key={item.to} item={item} />
                  ))}
                </SidebarMenu>
              </SidebarGroupContent>
            </SidebarGroup>
          ))}
        </nav>
      </SidebarContent>

      <SidebarFooter className="border-t border-sidebar-border group-data-[collapsible=icon]:px-3">
        <UserMenu collapsed={collapsed} />
        <div className={collapsed ? 'flex flex-col items-center gap-1' : 'flex items-center justify-between px-1'}>
          <ThemeToggle />
          {!isMobile && <SidebarTrigger aria-label={collapsed ? 'Expandir menú' : 'Contraer menú'} />}
        </div>
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  )
}
