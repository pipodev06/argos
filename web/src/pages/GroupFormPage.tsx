import { useNavigate, useParams } from 'react-router'
import { TableError } from '@/components/data-table/TableStates'
import { FormPage } from '@/components/ui/form-page'
import { Skeleton } from '@/components/ui/skeleton'
import { GroupForm } from '@/features/groups/components/GroupForm'
import { useGroups } from '@/features/groups/hooks/useGroups'

const BACK = { to: '/grupos', label: 'Grupos' }

/** Crear (`/grupos/nuevo`) o editar (`/grupos/:id`) un grupo. */
export function GroupFormPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { groups, error, refresh } = useGroups()
  const done = () => navigate(BACK.to)
  const group = id ? groups?.items.find((g) => g.id === id) : null

  return (
    <FormPage title={id ? 'Editar grupo' : 'Nuevo grupo'} back={BACK}>
      {error ? (
        <TableError message={error} onRetry={() => void refresh()} />
      ) : !groups ? (
        <Skeleton className="h-64 w-full" />
      ) : group === undefined ? (
        <p className="text-sm text-text-muted">El grupo no existe.</p>
      ) : (
        <GroupForm group={group} defaultDownAfter={groups.defaults.down_after_seconds} onCancel={done} onSaved={done} />
      )}
    </FormPage>
  )
}
