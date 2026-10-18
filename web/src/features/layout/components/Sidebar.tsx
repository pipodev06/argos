import { Link, NavLink, useMatch, useResolvedPath } from 'react-router'
import { AppLogoMark } from '@/components/ui/app-logo'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
} from '@/components/ui/sidebar'
import { useSidebar } from '@/components/ui/sidebar-context'
import { OpenAlertsCount } from '@/features/alerts/components/OpenAlertsCount'
import { NavUser } from '@/features/auth/components/NavUser'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { NAV_ITEMS, groupBySection, isNavItemVisible, type NavItem } from '../nav-items'

// Activo = fondo muted + semibold + barra de 2px en `text` (AGENTS.md); nunca brand.
const ACTIVE = 'relative text-text-muted hover:text-text data-active:font-semibold data-active:text-text'

function useIsActive(item: NavItem) {
  return useMatch({ path: useResolvedPath(item.to).pathname, end: item.end ?? false }) !== null
}

function useCloseOnMobile() {
  const { isMobile, setOpenMobile } = useSidebar()
  return () => isMobile && setOpenMobile(false)
}

function ActiveBar() {
  return <span aria-hidden className="absolute inset-y-1 left-0 w-0.5 rounded-full bg-text" />
}

/** Item del menu: contraido el sidebar, solo su icono con tooltip. */
function NavItemLink({ item }: { item: NavItem }) {
  const { Icon, to, label, end, openAlertsCount } = item
  const isActive = useIsActive(item)
  const close = useCloseOnMobile()
  return (
    <SidebarMenuItem>
      <SidebarMenuButton asChild isActive={isActive} tooltip={label} className={ACTIVE}>
        <NavLink to={to} end={end} onClick={close}>
          {isActive && <ActiveBar />}
          <Icon strokeWidth={1.5} aria-hidden />
          <span>{label}</span>
          {openAlertsCount && <OpenAlertsCount />}
        </NavLink>
      </SidebarMenuButton>
    </SidebarMenuItem>
  )
}

/** Sidebar unico sobre el bloque `sidebar-07` de shadcn: colapsa a iconos (56px); en movil es un drawer. */
export function AppSidebar() {
  const { user } = useAuth()
  const items = NAV_ITEMS.filter((item) => isNavItemVisible(item, user))
  const groups = groupBySection(items)

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader className="group-data-[collapsible=icon]:px-3">
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton size="lg" asChild tooltip="argos">
              <Link to="/">
                <AppLogoMark className="size-8" />
                <span className="grid flex-1 text-left leading-tight">
                  <span className="truncate text-sm font-semibold">argos</span>
                  <span className="truncate text-xs text-text-muted">Monitoreo</span>
                </span>
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>

      <SidebarContent>
        <nav aria-label="Principal">
          {groups.map(([section, list]) => (
            <SidebarGroup key={section} className="group-data-[collapsible=icon]:px-3">
              <SidebarGroupLabel>{section}</SidebarGroupLabel>
              <SidebarMenu aria-label={section}>
                {list.map((item) => (
                  <NavItemLink key={item.to} item={item} />
                ))}
              </SidebarMenu>
            </SidebarGroup>
          ))}
        </nav>
      </SidebarContent>

      <SidebarFooter className="group-data-[collapsible=icon]:px-3">
        <NavUser />
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  )
}
