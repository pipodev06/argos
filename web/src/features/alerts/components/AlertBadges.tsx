import { OctagonAlertIcon, TriangleAlertIcon, BellRingIcon, ArrowBigUpDashIcon, CircleCheckIcon, ClockArrowUpIcon, EyeIcon, InfoIcon, RepeatIcon, WrenchIcon, type LucideIcon as Icon } from 'lucide-react'
import { Badge, type BadgeTint } from '@/components/ui/badge'
import type { Alert, AlertSeverity } from '../types/alert.types'
import { alertStateLabel, SEVERITY_LABELS } from '../utils/format'

const severityStyles: Record<AlertSeverity, { Icon: Icon; tint: BadgeTint }> = {
  info: { Icon: InfoIcon, tint: 'info' },
  warn: { Icon: TriangleAlertIcon, tint: 'warn' },
  critical: { Icon: OctagonAlertIcon, tint: 'danger' },
}

export function SeverityBadge({ severity }: { severity: AlertSeverity }) {
  const { Icon, tint } = severityStyles[severity]
  return (
    <Badge Icon={Icon} tint={tint}>
      {SEVERITY_LABELS[severity]}
    </Badge>
  )
}

export function AlertStateBadge({ alert }: { alert: Pick<Alert, 'state' | 'suppressed'> }) {
  const { state, suppressed } = alert
  const style: { Icon: Icon; tint: BadgeTint } =
    suppressed && state !== 'resolved'
      ? { Icon: WrenchIcon, tint: 'muted' }
      : state === 'open'
        ? { Icon: BellRingIcon, tint: 'danger' }
        : state === 'acknowledged'
          ? { Icon: EyeIcon, tint: 'info' }
          : { Icon: CircleCheckIcon, tint: 'ok' }
  return (
    <Badge Icon={style.Icon} tint={style.tint}>
      {alertStateLabel(alert)}
    </Badge>
  )
}

/** Alerta intermitente (flapping): icono + texto, solo si `flapping`. */
export function FlappingBadge({ flapping }: { flapping: boolean }) {
  if (!flapping) return null
  return <Badge Icon={RepeatIcon}>Intermitente</Badge>
}

/** Alerta que escaló por no ser reconocida a tiempo: icono + texto, solo si `escalatedAt`. */
export function EscalatedBadge({ escalatedAt }: { escalatedAt: string | null }) {
  if (!escalatedAt) return null
  return (
    <Badge Icon={ArrowBigUpDashIcon} tint="warn">
      Escalada
    </Badge>
  )
}

/** En la tabla de reglas: "Escala a los N min" (solo si hay plazo). */
export function EscalationBadge({ seconds }: { seconds: number | null }) {
  if (seconds === null) return null
  return <Badge Icon={ClockArrowUpIcon}>Escala a los {Math.round((seconds / 60) * 100) / 100} min</Badge>
}
