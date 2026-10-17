import { useState } from 'react'
import { ChevronsUpDownIcon, KeyRoundIcon, LogOutIcon, MoonIcon, SunIcon } from 'lucide-react'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { InlineError } from '@/components/ui/inline-error'
import { SidebarMenu, SidebarMenuButton, SidebarMenuItem } from '@/components/ui/sidebar'
import { useSidebar } from '@/components/ui/sidebar-context'
import { useTheme } from '@/features/theme/hooks/useTheme'
import { useAuth } from '../hooks/useAuth'
import { useLogout } from '../hooks/useLogout'
import { roleLabel } from '../utils/format'
import { ChangePasswordDialog } from './ChangePasswordDialog'

function UserIdentity({ name, role }: { name: string; role: string }) {
  return (
    <>
      <Avatar className="size-8 rounded-md after:rounded-md">
        <AvatarFallback className="rounded-md text-xs font-semibold">{name.slice(0, 2).toUpperCase()}</AvatarFallback>
      </Avatar>
      <span className="grid flex-1 text-left leading-tight">
        <span className="truncate text-sm font-semibold">{name}</span>
        <span className="truncate text-xs text-text-muted">{role}</span>
      </span>
    </>
  )
}

/** Pie del sidebar (patron `nav-user` de shadcn sidebar-07): usuario + menu con clave, tema y cierre de sesion. */
export function NavUser() {
  const { user } = useAuth()
  const { error, logout } = useLogout()
  const { theme, toggleTheme } = useTheme()
  const { isMobile } = useSidebar()
  const [passwordOpen, setPasswordOpen] = useState(false)
  if (!user) return null
  const role = roleLabel(user.role)
  const ThemeIcon = theme === 'dark' ? SunIcon : MoonIcon

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        {/* modal={false}: el modo modal inyecta el <style> de react-remove-scroll y la CSP lo bloquea */}
        <DropdownMenu modal={false}>
          <DropdownMenuTrigger asChild>
            <SidebarMenuButton size="lg" tooltip={`${user.username} · ${role}`} className="data-[state=open]:bg-sidebar-accent">
              <UserIdentity name={user.username} role={role} />
              <ChevronsUpDownIcon className="ml-auto text-text-muted" strokeWidth={1.5} aria-hidden />
            </SidebarMenuButton>
          </DropdownMenuTrigger>
          <DropdownMenuContent className="min-w-56" side={isMobile ? 'bottom' : 'right'} align="end" sideOffset={4}>
            <DropdownMenuLabel className="flex items-center gap-2 font-normal">
              <UserIdentity name={user.username} role={role} />
            </DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuItem onSelect={() => setPasswordOpen(true)}>
                <KeyRoundIcon strokeWidth={1.5} aria-hidden />
                Cambiar contraseña
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={toggleTheme}>
                <ThemeIcon strokeWidth={1.5} aria-hidden />
                {theme === 'dark' ? 'Modo claro' : 'Modo oscuro'}
              </DropdownMenuItem>
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
            {/* preventDefault: el menu queda abierto para mostrar el error si el servidor no confirma el cierre */}
            <DropdownMenuItem
              onSelect={(event) => {
                event.preventDefault()
                void logout()
              }}
            >
              <LogOutIcon strokeWidth={1.5} aria-hidden />
              Cerrar sesión
            </DropdownMenuItem>
            {error && <InlineError className="px-2 py-1.5">{error}</InlineError>}
          </DropdownMenuContent>
        </DropdownMenu>
        <ChangePasswordDialog open={passwordOpen} onClose={() => setPasswordOpen(false)} />
      </SidebarMenuItem>
    </SidebarMenu>
  )
}
