import { KeyRoundIcon, LockKeyholeIcon, ShieldIcon, Trash2Icon, UserCheckIcon, UserXIcon } from 'lucide-react'
import { Cell, DataTable, HeadCell, HeadRow, Row, RowNumberCell, RowNumberHeadCell, TableBody, TableHeader } from '@/components/data-table/DataTable'
import { Button } from '@/components/ui/button'
import { roleLabel } from '@/features/auth/utils/format'
import { formatFullDate } from '@/features/hosts/utils/format'
import { accessLabel } from '../utils/permissions'
import type { ManagedUser } from '../types/user.types'
import { UserStatusBadge } from './UserStatusBadge'

export type UserAction = 'role' | 'toggle' | 'password' | 'permissions' | 'delete'

interface UsersTableProps {
  users: ManagedUser[]
  currentUserId: string | undefined
  onAction: (action: UserAction, user: ManagedUser) => void
}

export function UsersTable({ users, currentUserId, onAction }: UsersTableProps) {
  return (
    <DataTable caption="Usuarios" className="min-w-[56rem]">
      <TableHeader>
        <HeadRow>
          <RowNumberHeadCell />
          <HeadCell>Usuario</HeadCell>
          <HeadCell>Rol</HeadCell>
          <HeadCell>Acceso</HeadCell>
          <HeadCell>Estado</HeadCell>
          <HeadCell>Último acceso</HeadCell>
          <HeadCell>Creado</HeadCell>
          <HeadCell>Acciones</HeadCell>
        </HeadRow>
      </TableHeader>
      <TableBody>
        {users.map((u, i) => {
          const self = u.id === currentUserId
          return (
            <Row key={u.id}>
              <RowNumberCell n={i + 1} />
              <Cell className="font-semibold">
                {u.username}
                {self && <span className="font-sans font-normal text-text-muted"> (tú)</span>}
              </Cell>
              <Cell>{roleLabel(u.role)}</Cell>
              <Cell>{accessLabel(u)}</Cell>
              <Cell>
                <UserStatusBadge disabled={u.disabled} />
              </Cell>
              <Cell className="tabular-nums">{u.last_login_at ? formatFullDate(u.last_login_at) : 'Nunca'}</Cell>
              <Cell className="tabular-nums">{formatFullDate(u.created_at)}</Cell>
              <Cell>
                {u.role === 'viewer' && (
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`Permisos de ${u.username}`}
                    title="Permisos por grupo"
                    onClick={() => onAction('permissions', u)}
                  >
                    <LockKeyholeIcon strokeWidth={1.5} aria-hidden />
                  </Button>
                )}
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`Cambiar rol de ${u.username}`}
                  title={self ? 'No puedes cambiar tu propio rol' : 'Cambiar rol'}
                  disabled={self}
                  onClick={() => onAction('role', u)}
                >
                  <ShieldIcon strokeWidth={1.5} aria-hidden />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`${u.disabled ? 'Habilitar' : 'Deshabilitar'} a ${u.username}`}
                  title={self ? 'No puedes deshabilitarte' : u.disabled ? 'Habilitar' : 'Deshabilitar'}
                  disabled={self}
                  onClick={() => onAction('toggle', u)}
                >
                  {u.disabled ? <UserCheckIcon strokeWidth={1.5} aria-hidden /> : <UserXIcon strokeWidth={1.5} aria-hidden />}
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`Restablecer contraseña de ${u.username}`}
                  title={self ? 'Tu contraseña se cambia desde el menú de usuario' : 'Restablecer contraseña'}
                  disabled={self}
                  onClick={() => onAction('password', u)}
                >
                  <KeyRoundIcon strokeWidth={1.5} aria-hidden />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`Eliminar a ${u.username}`}
                  title={self ? 'No puedes eliminarte' : 'Eliminar'}
                  disabled={self}
                  onClick={() => onAction('delete', u)}
                >
                  <Trash2Icon strokeWidth={1.5} aria-hidden />
                </Button>
              </Cell>
            </Row>
          )
        })}
      </TableBody>
    </DataTable>
  )
}
