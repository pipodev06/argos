import { useState } from 'react'
import { LockIcon, UsersIcon } from 'lucide-react'
import { NoResultsState, TableError } from '@/components/data-table/TableStates'
import { FilterBar } from '@/components/filters/FilterBar'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/ui/empty-state'
import { PageHeader } from '@/components/ui/page-header'
import { Skeleton } from '@/components/ui/skeleton'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { ChangeRoleDialog } from '@/features/users/components/ChangeRoleDialog'
import { CreateUserDialog } from '@/features/users/components/CreateUserDialog'
import { DeleteUserDialog } from '@/features/users/components/DeleteUserDialog'
import { PermissionsDialog } from '@/features/users/components/PermissionsDialog'
import { ResetPasswordDialog } from '@/features/users/components/ResetPasswordDialog'
import { ToggleUserDialog } from '@/features/users/components/ToggleUserDialog'
import { UsersTable, type UserAction } from '@/features/users/components/UsersTable'
import { useUsers } from '@/features/users/hooks/useUsers'
import type { ManagedUser } from '@/features/users/types/user.types'
import { filterUsers, USER_FIELDS } from '@/features/users/utils/userQuery'
import { useUrlFilters } from '@/hooks/useUrlFilters'

/** Solo administradores: un viewer ve el aviso y no se monta nada que llame a la API. */
export function UsersPage() {
  const { user } = useAuth()
  if (user?.role !== 'admin') {
    return (
      <>
        <PageHeader title="Usuarios" />
        <EmptyState Icon={LockIcon} title="Sin permiso" />
      </>
    )
  }
  return <UsersAdmin currentUserId={user.id} />
}

function UsersAdmin({ currentUserId }: { currentUserId: string }) {
  const { users, error, loading, refresh } = useUsers()
  const { filters, setFilters, clear, hasActive } = useUrlFilters(USER_FIELDS)
  const [creating, setCreating] = useState(false)
  const [target, setTarget] = useState<{ action: UserAction; user: ManagedUser } | null>(null)

  const visible = users ? filterUsers(users, filters) : null
  const pick = (action: UserAction) => (target?.action === action ? target.user : null)

  function done() {
    setCreating(false)
    setTarget(null)
    void refresh()
  }

  return (
    <>
      <PageHeader title="Usuarios" actions={<Button size="sm" onClick={() => setCreating(true)}>Nuevo usuario</Button>} />
      <div className="flex flex-col gap-4" aria-busy={loading}>
        <FilterBar
          fields={USER_FIELDS}
          values={filters}
          onChange={setFilters}
          onClear={clear}
          itemLabel="usuarios"
          total={visible?.length ?? null}
          overall={users?.length ?? null}
        />
        {error && <TableError message={error} onRetry={() => void refresh()} />}
        {loading && !users && <Skeleton className="h-40 w-full" />}
        {users && users.length === 0 && <EmptyState Icon={UsersIcon} title="Sin usuarios" />}
        {visible && users && users.length > 0 && visible.length === 0 && hasActive && (
          <NoResultsState Icon={UsersIcon} onClear={clear} />
        )}
        {visible && visible.length > 0 && (
          <UsersTable
            users={visible}
            currentUserId={currentUserId}
            onAction={(action, u) => setTarget({ action, user: u })}
          />
        )}
      </div>
      <CreateUserDialog open={creating} onClose={() => setCreating(false)} onSaved={done} />
      <ChangeRoleDialog user={pick('role')} onClose={() => setTarget(null)} onSaved={done} />
      <PermissionsDialog user={pick('permissions')} onClose={() => setTarget(null)} onSaved={done} />
      <ToggleUserDialog user={pick('toggle')} onClose={() => setTarget(null)} onSaved={done} />
      <DeleteUserDialog user={pick('delete')} onClose={() => setTarget(null)} onSaved={done} />
      <ResetPasswordDialog user={pick('password')} onClose={() => setTarget(null)} onSaved={done} />
    </>
  )
}
