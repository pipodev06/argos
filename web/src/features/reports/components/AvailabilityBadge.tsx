import { IconAlertTriangle, IconCircleCheck, IconCircleOff } from '@tabler/icons-react'
import { Badge } from '@/components/ui/badge'
import { AVAILABILITY_TARGET_PCT, formatAvailabilityPercent } from '../utils/format'

/** Disponibilidad con estado (icono + texto, el color solo en icono y tinte): cumple, no cumple o sin datos. */
export function AvailabilityBadge({ pct }: { pct: number | null }) {
  const props =
    pct === null
      ? ({ Icon: IconCircleOff, tint: 'muted' } as const)
      : pct >= AVAILABILITY_TARGET_PCT
        ? ({ Icon: IconCircleCheck, tint: 'ok' } as const)
        : ({ Icon: IconAlertTriangle, tint: 'warn' } as const)
  return <Badge {...props}>{formatAvailabilityPercent(pct)}</Badge>
}
