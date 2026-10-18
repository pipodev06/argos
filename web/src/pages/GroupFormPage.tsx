import { useNavigate, useParams } from 'react-router'
import { TableError } from '@/components/data-table/TableStates'
import { FormPage } from '@/components/ui/form-page'
import type { HelpItem } from '@/components/ui/form-help'
import { MAX_DETAIL_DAYS, MIN_DETAIL_DAYS } from '@/features/retention/utils/retention'
import { Skeleton } from '@/components/ui/skeleton'
import { GroupForm } from '@/features/groups/components/GroupForm'
import { useGroups } from '@/features/groups/hooks/useGroups'

const GROUP_HELP: readonly HelpItem[] = [
  ['Considerar sin reportar tras', 'Tiempo sin reportes para cambiar de estado. Vacío = valor global.'],
  ['Caído', 'Para equipos que deben estar siempre encendidos: cuenta como falla.'],
  ['Sin conexión', 'Para equipos que se apagan o pierden red con normalidad: no cuenta como falla.'],
  ['Retención de detalle', `Días (${MIN_DETAIL_DAYS} a ${MAX_DETAIL_DAYS}) de métricas cada 30 s; después solo queda el resumen por hora. "Heredar" usa la global.`],
  ['Cuotas del cliente', 'Vacío = sin límite propio. Solo limita lo nuevo, no lo que ya existe.'],
]

const BACK = { to: '/grupos', label: 'Grupos' }

/** Crear (`/grupos/nuevo`) o editar (`/grupos/:id`) un grupo. */
export function GroupFormPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { groups, error, refresh } = useGroups()
  const done = () => navigate(BACK.to)
  const group = id ? groups?.items.find((g) => g.id === id) : null

  return (
    <FormPage title={id ? 'Editar grupo' : 'Nuevo grupo'} back={BACK} help={GROUP_HELP}>
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
