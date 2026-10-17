import { TriangleAlertIcon, CircleCheckIcon, CircleOffIcon } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { AVAILABILITY_TARGET_PCT, formatAvailabilityPercent } from '../utils/format'

/** Disponibilidad con estado (icono + texto, el color solo en icono y tinte): cumple, no cumple o sin datos. */
export function AvailabilityBadge({ pct }: { pct: number | null }) {
  const props =
    pct === null
      ? ({ Icon: CircleOffIcon, tint: 'muted' } as const)
      : pct >= AVAILABILITY_TARGET_PCT
        ? ({ Icon: CircleCheckIcon, tint: 'ok' } as const)
        : ({ Icon: TriangleAlertIcon, tint: 'warn' } as const)
  return <Badge {...props}>{formatAvailabilityPercent(pct)}</Badge>
}
