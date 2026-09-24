import { useState } from 'react'
import { IconKey, IconLogout, IconUser } from '@tabler/icons-react'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { InlineError } from '@/components/ui/inline-error'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { useAuth } from '../hooks/useAuth'
import { useLogout } from '../hooks/useLogout'
import { roleLabel } from '../utils/format'
import { ChangePasswordDialog } from './ChangePasswordDialog'

// Pie del sidebar: usuario y rol actuales; el menu ofrece cambio de clave y cierre de sesion.
// `collapsed` = sidebar de 56px: solo icono con tooltip.
export function UserMenu({ collapsed = false }: { collapsed?: boolean }) {
  const { user } = useAuth()
  const { error, logout } = useLogout()
  const [passwordOpen, setPasswordOpen] = useState(false)

  if (!user) return null

  const summary = `${user.username} · ${roleLabel(user.role)}`
  const trigger = collapsed ? (
    <Button variant="ghost" size="icon-sm" aria-label={`Cuenta: ${summary}`}>
      <IconUser stroke={1.5} aria-hidden />
    </Button>
  ) : (
    <Button variant="ghost" className="h-auto w-full justify-start gap-2 px-2 py-1.5 text-left">
      <IconUser className="text-text-muted" stroke={1.5} aria-hidden />
      <span className="flex min-w-0 flex-col">
        <span className="truncate text-sm font-semibold">{user.username}</span>
        <span className="truncate text-xs font-normal text-text-muted">{roleLabel(user.role)}</span>
      </span>
    </Button>
  )

  return (
    <>
      {/* modal={false}: el modo modal inyecta el <style> de react-remove-scroll y la CSP lo bloquea */}
      <DropdownMenu modal={false}>
        {collapsed ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <DropdownMenuTrigger asChild>{trigger}</DropdownMenuTrigger>
            </TooltipTrigger>
            <TooltipContent side="right">{summary}</TooltipContent>
          </Tooltip>
        ) : (
          <DropdownMenuTrigger asChild>{trigger}</DropdownMenuTrigger>
        )}
        <DropdownMenuContent side="top" align="start">
          <DropdownMenuLabel>{summary}</DropdownMenuLabel>
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={() => setPasswordOpen(true)}>
            <IconKey stroke={1.5} aria-hidden />
            Cambiar contraseña
          </DropdownMenuItem>
          {/* preventDefault: el menu queda abierto para mostrar el error si el servidor no confirma el cierre */}
          <DropdownMenuItem
            onSelect={(event) => {
              event.preventDefault()
              void logout()
            }}
          >
            <IconLogout stroke={1.5} aria-hidden />
            Cerrar sesión
          </DropdownMenuItem>
          {error && <InlineError className="px-2 py-1.5">{error}</InlineError>}
        </DropdownMenuContent>
      </DropdownMenu>
      <ChangePasswordDialog open={passwordOpen} onClose={() => setPasswordOpen(false)} />
    </>
  )
}
