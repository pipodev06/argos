import { IconAlertTriangle, IconCircleCheck, IconCircleX, IconHelpCircle, IconPlayerPause, type Icon } from '@tabler/icons-react'
import { Badge, type BadgeTint } from '@/components/ui/badge'
import type { CheckStatus } from '../types/check.types'

/** `paused` no viene del servidor: lo usa el detalle para un chequeo deshabilitado. */
const styles: Record<CheckStatus | 'paused', { Icon: Icon; tint: BadgeTint; label: string }> = {
  up: { Icon: IconCircleCheck, tint: 'ok', label: 'Activo' },
  down: { Icon: IconCircleX, tint: 'danger', label: 'Caído' },
  warn: { Icon: IconAlertTriangle, tint: 'warn', label: 'Advertencia' },
  unknown: { Icon: IconHelpCircle, tint: 'muted', label: 'Desconocido' },
  paused: { Icon: IconPlayerPause, tint: 'muted', label: 'Pausado' },
}

export function CheckStatusBadge({ status }: { status: CheckStatus | 'paused' }) {
  const { Icon, tint, label } = styles[status]
  return (
    <Badge Icon={Icon} tint={tint}>
      {label}
    </Badge>
  )
}
