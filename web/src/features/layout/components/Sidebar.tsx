import { ChevronRightIcon } from 'lucide-react'
import { Link, NavLink, useLocation, useMatch, useResolvedPath } from 'react-router'
import { AppLogoMark } from '@/components/ui/app-logo'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
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
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
  SidebarRail,
} from '@/components/ui/sidebar'
import { useSidebar } from '@/components/ui/sidebar-context'
import { OpenAlertsCount } from '@/features/alerts/components/OpenAlertsCount'
import { NavUser } from '@/features/auth/components/NavUser'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { COLLAPSIBLE_SECTIONS, NAV_ITEMS, groupBySection, isNavItemVisible, type NavItem } from '../nav-items'

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

/** Item de primer nivel (seccion "Monitoreo"). */
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

function NavSubLink({ item }: { item: NavItem }) {
  const isActive = useIsActive(item)
  const close = useCloseOnMobile()
  return (
    <SidebarMenuSubItem>
      <SidebarMenuSubButton asChild isActive={isActive} className={ACTIVE}>
        <NavLink to={item.to} end={item.end} onClick={close}>
          <span>{item.subLabel ?? item.label}</span>
          {item.openAlertsCount && <OpenAlertsCount />}
        </NavLink>
      </SidebarMenuSubButton>
    </SidebarMenuSubItem>
  )
}

/** Seccion plegable (patron `nav-main` de sidebar-07). Contraido a iconos, el icono lleva a su primera pagina. */
function NavSection({ section, items }: { section: string; items: NavItem[] }) {
  const Icon = COLLAPSIBLE_SECTIONS[section]
  const { pathname } = useLocation()
  const { state, isMobile } = useSidebar()
  const containsActive = items.some((i) => (i.end ? pathname === i.to : pathname.startsWith(i.to)))
  if (state === 'collapsed' && !isMobile) {
    return (
      <SidebarMenuItem>
        <SidebarMenuButton asChild isActive={containsActive} tooltip={section} className={ACTIVE}>
          <Link to={items[0].to}>
            {containsActive && <ActiveBar />}
            <Icon strokeWidth={1.5} aria-hidden />
            <span>{section}</span>
          </Link>
        </SidebarMenuButton>
      </SidebarMenuItem>
    )
  }
  return (
    <Collapsible asChild defaultOpen={containsActive} className="group/collapsible">
      <SidebarMenuItem>
        <CollapsibleTrigger asChild>
          <SidebarMenuButton tooltip={section} className="text-text-muted hover:text-text">
            <Icon strokeWidth={1.5} aria-hidden />
            <span>{section}</span>
            <ChevronRightIcon
              className="ml-auto transition-transform duration-200 group-data-[state=open]/collapsible:rotate-90"
              strokeWidth={1.5}
              aria-hidden
            />
          </SidebarMenuButton>
        </CollapsibleTrigger>
        <CollapsibleContent>
          <SidebarMenuSub>
            {items.map((item) => (
              <NavSubLink key={item.to} item={item} />
            ))}
          </SidebarMenuSub>
        </CollapsibleContent>
      </SidebarMenuItem>
    </Collapsible>
  )
}

/** Sidebar unico sobre el bloque `sidebar-07` de shadcn: colapsa a iconos (56px); en movil es un drawer. */
export function AppSidebar() {
  const { user } = useAuth()
  const items = NAV_ITEMS.filter((item) => isNavItemVisible(item, user))
  const groups = groupBySection(items)
  const flat = groups.filter(([section]) => !COLLAPSIBLE_SECTIONS[section])
  const nested = groups.filter(([section]) => COLLAPSIBLE_SECTIONS[section])

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
          {flat.map(([section, list]) => (
            <SidebarGroup key={section} className="group-data-[collapsible=icon]:px-3">
              <SidebarGroupLabel>{section}</SidebarGroupLabel>
              <SidebarMenu aria-label={section}>
                {list.map((item) => (
                  <NavItemLink key={item.to} item={item} />
                ))}
              </SidebarMenu>
            </SidebarGroup>
          ))}
          {nested.length > 0 && (
            <SidebarGroup className="group-data-[collapsible=icon]:px-3">
              <SidebarGroupLabel>Gestión</SidebarGroupLabel>
              <SidebarMenu aria-label="Gestión">
                {nested.map(([section, list]) => (
                  <NavSection key={section} section={section} items={list} />
                ))}
              </SidebarMenu>
            </SidebarGroup>
          )}
        </nav>
      </SidebarContent>

      <SidebarFooter className="group-data-[collapsible=icon]:px-3">
        <NavUser />
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  )
}
