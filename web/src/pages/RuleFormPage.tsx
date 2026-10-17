import { useNavigate, useParams, useSearchParams } from 'react-router'
import { TableError } from '@/components/data-table/TableStates'
import { FormPage } from '@/components/ui/form-page'
import { Skeleton } from '@/components/ui/skeleton'
import { RuleForm } from '@/features/alerts/components/RuleForm'
import { useAlertRules } from '@/features/alerts/hooks/useAlerts'

const BACK = { to: '/alertas/reglas', label: 'Reglas' }

/** Crear (`/alertas/reglas/nueva`, `?clonar=<id>` para duplicar) o editar (`/alertas/reglas/:id`) una regla. */
export function RuleFormPage() {
  const { id } = useParams()
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const sourceId = id ?? params.get('clonar')
  const { data: rules, error, refresh } = useAlertRules()
  const done = () => navigate(BACK.to)

  if (!sourceId) {
    return (
      <FormPage title="Nueva regla" back={BACK}>
        <RuleForm rule={null} onCancel={done} onSaved={done} />
      </FormPage>
    )
  }
  const rule = rules?.find((r) => r.id === sourceId)
  const title = id ? 'Editar regla' : 'Duplicar regla'
  return (
    <FormPage title={title} back={BACK}>
      {error ? (
        <TableError message={error} onRetry={() => void refresh()} />
      ) : !rules ? (
        <Skeleton className="h-64 w-full" />
      ) : !rule ? (
        <p className="text-sm text-text-muted">La regla no existe.</p>
      ) : (
        <RuleForm rule={rule} clone={!id} onCancel={done} onSaved={done} />
      )}
    </FormPage>
  )
}
