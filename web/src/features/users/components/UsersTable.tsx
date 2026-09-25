import { IconKey, IconShield, IconUserCheck, IconUserOff } from '@tabler/icons-react'
import { Cell, DataTable, HeadCell, HeadRow, Row } from '@/components/data-table/DataTable'
import { Button } from '@/components/ui/button'
import { roleLabel } from '@/features/auth/utils/format'
import { formatFullDate } from '@/features/hosts/utils/format'
import type { ManagedUser } from '../types/user.types'
import { UserStatusBadge } from './UserStatusBadge'

export type UserAction = 'role' | 'toggle' | 'password'

interface UsersTableProps {
  users: ManagedUser[]
  currentUserId: string | undefined
  onAction: (action: UserAction, user: ManagedUser) => void
}

export function UsersTable({ users, currentUserId, onAction }: UsersTableProps) {
  return (
    <DataTable caption="Usuarios" className="min-w-[48rem]">
      <thead>
        <HeadRow>
          <HeadCell>Usuario</HeadCell>
          <HeadCell>Rol</HeadCell>
          <HeadCell>Estado</HeadCell>
          <HeadCell>Último acceso</HeadCell>
          <HeadCell>Creado</HeadCell>
          <HeadCell>Acciones</HeadCell>
        </HeadRow>
      </thead>
      <tbody>
        {users.map((u) => {
          const self = u.id === currentUserId
          return (
            <Row key={u.id}>
              <Cell className="font-semibold">
                {u.username}
                {self && <span className="font-sans font-normal text-text-muted"> (tú)</span>}
              </Cell>
              <Cell>{roleLabel(u.role)}</Cell>
              <Cell>
                <UserStatusBadge disabled={u.disabled} />
              </Cell>
              <Cell className="tabular-nums">{u.last_login_at ? formatFullDate(u.last_login_at) : 'Nunca'}</Cell>
              <Cell className="tabular-nums">{formatFullDate(u.created_at)}</Cell>
              <Cell>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`Cambiar rol de ${u.username}`}
                  title={self ? 'No puedes cambiar tu propio rol' : 'Cambiar rol'}
                  disabled={self}
                  onClick={() => onAction('role', u)}
                >
                  <IconShield stroke={1.5} aria-hidden />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`${u.disabled ? 'Habilitar' : 'Deshabilitar'} a ${u.username}`}
                  title={self ? 'No puedes deshabilitarte' : u.disabled ? 'Habilitar' : 'Deshabilitar'}
                  disabled={self}
                  onClick={() => onAction('toggle', u)}
                >
                  {u.disabled ? <IconUserCheck stroke={1.5} aria-hidden /> : <IconUserOff stroke={1.5} aria-hidden />}
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`Restablecer contraseña de ${u.username}`}
                  title={self ? 'Tu contraseña se cambia desde el menú de usuario' : 'Restablecer contraseña'}
                  disabled={self}
                  onClick={() => onAction('password', u)}
                >
                  <IconKey stroke={1.5} aria-hidden />
                </Button>
              </Cell>
            </Row>
          )
        })}
      </tbody>
    </DataTable>
  )
}
