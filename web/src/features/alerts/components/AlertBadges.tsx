import type { ComponentType } from 'react'
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
} from '@tabler/icons-react'
import { formatClock } from '@/features/hosts/utils/format'
import type { Alert, AlertSeverity } from '../types/alert.types'
import { alertStateLabel, SEVERITY_LABELS } from '../utils/format'

type BadgeIcon = ComponentType<{ className?: string; stroke?: number; 'aria-hidden'?: boolean }>

const BADGE = 'inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-xs font-medium text-text'

// El color de estado va solo en el icono (y el tinte de fondo); el texto usa siempre `text`.
const severityStyles: Record<AlertSeverity, { Icon: BadgeIcon; badge: string; icon: string }> = {
  info: { Icon: IconInfoCircle, badge: 'bg-info/10', icon: 'text-info' },
  warn: { Icon: IconAlertTriangle, badge: 'bg-warn/10', icon: 'text-warn' },
  critical: { Icon: IconAlertOctagon, badge: 'bg-danger/10', icon: 'text-danger' },
}

export function SeverityBadge({ severity }: { severity: AlertSeverity }) {
  const { Icon, badge, icon } = severityStyles[severity]
  return (
    <span className={`${BADGE} ${badge}`}>
      <Icon className={`size-3.5 shrink-0 ${icon}`} stroke={1.5} aria-hidden />
      {SEVERITY_LABELS[severity]}
    </span>
  )
}

export function AlertStateBadge({ alert }: { alert: Pick<Alert, 'state' | 'suppressed'> }) {
  const { state, suppressed } = alert
  const style =
    suppressed && state !== 'resolved'
      ? { Icon: IconTool, badge: 'bg-text-muted/10', icon: 'text-text-muted' }
      : state === 'open'
        ? { Icon: IconBellRinging, badge: 'bg-danger/10', icon: 'text-danger' }
        : state === 'acknowledged'
          ? { Icon: IconEye, badge: 'bg-info/10', icon: 'text-info' }
          : { Icon: IconCircleCheck, badge: 'bg-ok/10', icon: 'text-ok' }
  return (
    <span className={`${BADGE} ${style.badge}`}>
      <style.Icon className={`size-3.5 shrink-0 ${style.icon}`} stroke={1.5} aria-hidden />
      {alertStateLabel(alert)}
    </span>
  )
}

/** Alerta intermitente (flapping): icono + texto, solo si `flapping`. */
export function FlappingBadge({ flapping }: { flapping: boolean }) {
  if (!flapping) return null
  return (
    <span className={`${BADGE} bg-text-muted/10`}>
      <IconRepeat className="size-3.5 shrink-0 text-text-muted" stroke={1.5} aria-hidden />
      Intermitente
    </span>
  )
}

/** Alerta que escaló por no ser reconocida a tiempo: icono + texto, solo si `escalatedAt`. */
export function EscalatedBadge({ escalatedAt }: { escalatedAt: string | null }) {
  if (!escalatedAt) return null
  return (
    <span className={`${BADGE} bg-warn/10`}>
      <IconArrowBigUpLines className="size-3.5 shrink-0 text-warn" stroke={1.5} aria-hidden />
      Escalada
    </span>
  )
}

/** En la tabla de reglas: "Escala a los N min" (solo si hay plazo). */
export function EscalationBadge({ seconds }: { seconds: number | null }) {
  if (seconds === null) return null
  return (
    <span className={`${BADGE} bg-text-muted/10`}>
      <IconClockUp className="size-3.5 shrink-0 text-text-muted" stroke={1.5} aria-hidden />
      Escala a los {Math.round((seconds / 60) * 100) / 100} min
    </span>
  )
}

/** Junto al estado de una regla silenciada: icono + "Silenciada hasta HH:mm" (nunca solo color). */
export function MutedBadge({ mutedUntil }: { mutedUntil: string }) {
  return (
    <span className={`${BADGE} bg-text-muted/10`}>
      <IconBellPause className="size-3.5 shrink-0 text-text-muted" stroke={1.5} aria-hidden />
      Silenciada hasta {formatClock(new Date(mutedUntil).getTime())}
    </span>
  )
}
