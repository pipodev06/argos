import type { ComponentType } from 'react'
import {
  IconAlertOctagon,
  IconAlertTriangle,
  IconBellRinging,
  IconCircleCheck,
  IconEye,
  IconInfoCircle,
  IconTool,
} from '@tabler/icons-react'
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
