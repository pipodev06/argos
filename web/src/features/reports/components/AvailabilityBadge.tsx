import { IconAlertTriangle, IconCircleCheck, IconCircleOff } from '@tabler/icons-react'
import { AVAILABILITY_TARGET_PCT, formatAvailabilityPercent } from '../utils/format'

const BADGE = 'inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-xs font-medium text-text'

/** Disponibilidad con estado (icono + texto, el color solo en icono y tinte): cumple, no cumple o sin datos. */
export function AvailabilityBadge({ pct }: { pct: number | null }) {
  const { Icon, badge, icon } =
    pct === null
      ? { Icon: IconCircleOff, badge: 'bg-text-muted/10', icon: 'text-text-muted' }
      : pct >= AVAILABILITY_TARGET_PCT
        ? { Icon: IconCircleCheck, badge: 'bg-ok/10', icon: 'text-ok' }
        : { Icon: IconAlertTriangle, badge: 'bg-warn/10', icon: 'text-warn' }
  return (
    <span className={`${BADGE} ${badge}`}>
      <Icon className={`size-3.5 shrink-0 ${icon}`} stroke={1.5} aria-hidden />
      {formatAvailabilityPercent(pct)}
    </span>
  )
}
