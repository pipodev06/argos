import { TriangleAlertIcon, CircleCheckIcon, CircleXIcon, CircleHelpIcon, PauseIcon, type LucideIcon as Icon } from 'lucide-react'
import { Badge, type BadgeTint } from '@/components/ui/badge'
import type { CheckStatus } from '../types/check.types'

/** `paused` no viene del servidor: lo usa el detalle para un chequeo deshabilitado. */
const styles: Record<CheckStatus | 'paused', { Icon: Icon; tint: BadgeTint; label: string }> = {
  up: { Icon: CircleCheckIcon, tint: 'ok', label: 'Activo' },
  down: { Icon: CircleXIcon, tint: 'danger', label: 'Caído' },
  warn: { Icon: TriangleAlertIcon, tint: 'warn', label: 'Advertencia' },
  unknown: { Icon: CircleHelpIcon, tint: 'muted', label: 'Desconocido' },
  paused: { Icon: PauseIcon, tint: 'muted', label: 'Pausado' },
}

export function CheckStatusBadge({ status }: { status: CheckStatus | 'paused' }) {
  const { Icon, tint, label } = styles[status]
  return (
    <Badge Icon={Icon} tint={tint}>
      {label}
    </Badge>
  )
}
