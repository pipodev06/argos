import {
  IconAlertOctagon,
  IconAlertTriangle,
  IconBellPause,
  IconBellRinging,
  IconArrowBigUpLines,
  IconCircleCheck,
  IconClockUp,
  IconEye,
  IconInfoCircle,
  IconRepeat,
  IconTool,
  type Icon,
} from '@tabler/icons-react'
import { Badge, type BadgeTint } from '@/components/ui/badge'
import { formatClock } from '@/features/hosts/utils/format'
import type { Alert, AlertSeverity } from '../types/alert.types'
import { alertStateLabel, SEVERITY_LABELS } from '../utils/format'

const severityStyles: Record<AlertSeverity, { Icon: Icon; tint: BadgeTint }> = {
  info: { Icon: IconInfoCircle, tint: 'info' },
  warn: { Icon: IconAlertTriangle, tint: 'warn' },
  critical: { Icon: IconAlertOctagon, tint: 'danger' },
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
      ? { Icon: IconTool, tint: 'muted' }
      : state === 'open'
        ? { Icon: IconBellRinging, tint: 'danger' }
        : state === 'acknowledged'
          ? { Icon: IconEye, tint: 'info' }
          : { Icon: IconCircleCheck, tint: 'ok' }
  return (
    <Badge Icon={style.Icon} tint={style.tint}>
      {alertStateLabel(alert)}
    </Badge>
  )
}

/** Alerta intermitente (flapping): icono + texto, solo si `flapping`. */
export function FlappingBadge({ flapping }: { flapping: boolean }) {
  if (!flapping) return null
  return <Badge Icon={IconRepeat}>Intermitente</Badge>
}

/** Alerta que escaló por no ser reconocida a tiempo: icono + texto, solo si `escalatedAt`. */
export function EscalatedBadge({ escalatedAt }: { escalatedAt: string | null }) {
  if (!escalatedAt) return null
  return (
    <Badge Icon={IconArrowBigUpLines} tint="warn">
      Escalada
    </Badge>
  )
}

/** En la tabla de reglas: "Escala a los N min" (solo si hay plazo). */
export function EscalationBadge({ seconds }: { seconds: number | null }) {
  if (seconds === null) return null
  return <Badge Icon={IconClockUp}>Escala a los {Math.round((seconds / 60) * 100) / 100} min</Badge>
}

/** Junto al estado de una regla silenciada: icono + "Silenciada hasta HH:mm" (nunca solo color). */
export function MutedBadge({ mutedUntil }: { mutedUntil: string }) {
  return <Badge Icon={IconBellPause}>Silenciada hasta {formatClock(new Date(mutedUntil).getTime())}</Badge>
}
