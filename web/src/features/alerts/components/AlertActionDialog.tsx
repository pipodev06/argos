import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import { useUserMutation } from '@/features/users/hooks/useUserMutation'
import type { ApiError } from '@/lib/api'
import { alertAction } from '../services/alerts.service'
import type { Alert } from '../types/alert.types'

export type AlertActionKind = 'ack' | 'resolve'

interface AlertActionDialogProps {
  target: { action: AlertActionKind; alert: Alert } | null
  /** Cancelar, éxito o cierre tras error: quien lo usa cierra y refresca la vista. */
  onClose: () => void
}

const ALREADY_RESOLVED = 'La alerta ya estaba resuelta (por el sistema u otro administrador). Actualiza la vista.'
const NO_PERMISSION = 'No tienes permiso para esta acción. Pide a un administrador que te habilite "Reconocer alertas".'
const mapError = (err: ApiError) => {
  if (err.code === 'alert_resolved') return ALREADY_RESOLVED
  return err.status === 403 ? NO_PERMISSION : null
}

/** Confirmación de Reconocer / Resolver una alerta (Reconocer: admin o con permiso; Resolver: solo admin). */
export function AlertActionDialog({ target, onClose }: AlertActionDialogProps) {
  const { error, busy, run, clearError } = useUserMutation()
  const resolving = target?.action === 'resolve'
  const name = target ? `"${target.alert.rule_name}" en ${target.alert.host.hostname}` : ''

  function close() {
    clearError()
    onClose()
  }

  async function confirm() {
    if (!target) return
    const ok = await run(() => alertAction(target.alert.id, target.action), mapError)
    if (ok) close()
  }

  return (
    <ConfirmDialog
      open={target !== null}
      title={resolving ? 'Resolver alerta' : 'Reconocer alerta'}
      description={
        resolving
          ? `Se marca como resuelta ${name}. Si la condición sigue presente, se abrirá una alerta nueva en el siguiente ciclo.`
          : `Se marca como reconocida ${name}: indica que alguien la está atendiendo.`
      }
      confirmLabel={resolving ? 'Resolver' : 'Reconocer'}
      busy={busy}
      error={error}
      onConfirm={() => void confirm()}
      onClose={close}
    />
  )
}
