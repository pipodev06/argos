import { useNavigate, useParams } from 'react-router'
import { TableError } from '@/components/data-table/TableStates'
import { FormPage } from '@/components/ui/form-page'
import { Skeleton } from '@/components/ui/skeleton'
import { CheckForm } from '@/features/checks/components/CheckForm'
import { useServiceChecks } from '@/features/checks/hooks/useChecks'

const BACK = { to: '/chequeos', label: 'Chequeos' }

/** Crear (`/chequeos/nuevo`) o editar (`/chequeos/:id`) un chequeo de servicio. */
export function CheckFormPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { data: checks, error, refresh } = useServiceChecks()
  const done = () => navigate(BACK.to)

  if (!id) {
    return (
      <FormPage title="Nuevo chequeo" back={BACK}>
        <CheckForm check={null} onCancel={done} onSaved={done} />
      </FormPage>
    )
  }
  const check = checks?.find((c) => c.id === id)
  return (
    <FormPage title="Editar chequeo" back={BACK}>
      {error ? (
        <TableError message={error} onRetry={() => void refresh()} />
      ) : !checks ? (
        <Skeleton className="h-64 w-full" />
      ) : !check ? (
        <p className="text-sm text-text-muted">El chequeo no existe.</p>
      ) : (
        <CheckForm check={check} onCancel={done} onSaved={done} />
      )}
    </FormPage>
  )
}
