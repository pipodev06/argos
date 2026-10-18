import { useNavigate, useParams } from 'react-router'
import { TableError } from '@/components/data-table/TableStates'
import { FormPage } from '@/components/ui/form-page'
import type { HelpItem } from '@/components/ui/form-help'
import { MAX_IF_INDEX } from '@/features/checks/utils/checkForm'
import { SNMP_HOWTO } from '@/features/snmp-profiles/utils/howto'
import { Skeleton } from '@/components/ui/skeleton'
import { CheckForm } from '@/features/checks/components/CheckForm'
import { useServiceChecks } from '@/features/checks/hooks/useChecks'
import { useBreadcrumbTrail } from '@/features/layout/hooks/breadcrumbContext'

const CHECK_HELP: readonly HelpItem[] = [
  ['Tipo', 'No se puede cambiar después: crea otro chequeo si lo necesitas distinto.'],
  ['URL', 'Sin usuario ni clave y sin tokens en la query: el token va en el secreto de cabecera.'],
  ['Estado esperado', 'Un código o un rango. Vacío = 200-399.'],
  ['Texto esperado', 'Opcional: debe aparecer en la respuesta.'],
  ['Advertencia / Crítico', 'Opcionales: tiempo de respuesta (ms) o días restantes del certificado.'],
  ['Secreto de cabecera', 'Se guarda cifrado y no se vuelve a mostrar; al editar, vacío conserva el guardado.'],
  ['SNMP', `El dispositivo necesita un perfil SNMP (en su formulario). Sin umbrales. ifIndex: entero de 1 a ${MAX_IF_INDEX}.`],
  ['Intervalo, Timeout, Reintentos', '30 a 86 400 s; 200 a 30 000 ms; 0 a 3.'],
  ['Chequeo activo', 'Desactivado, el agente deja de ejecutarlo.'],
  ...SNMP_HOWTO,
]

const BACK = { to: '/chequeos', label: 'Chequeos' }

/** Crear (`/chequeos/nuevo`) o editar (`/chequeos/:id`) un chequeo de servicio. */
export function CheckFormPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { data: checks, error, refresh } = useServiceChecks()
  const done = () => navigate(BACK.to)
  const check = id ? checks?.find((c) => c.id === id) : undefined
  useBreadcrumbTrail(!id ? [{ label: 'Nuevo' }] : check ? [{ label: check.name }] : null)

  if (!id) {
    return (
      <FormPage title="Nuevo chequeo" back={BACK} help={CHECK_HELP}>
        <CheckForm check={null} onCancel={done} onSaved={done} />
      </FormPage>
    )
  }
  return (
    <FormPage title="Editar chequeo" back={BACK} help={CHECK_HELP}>
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
