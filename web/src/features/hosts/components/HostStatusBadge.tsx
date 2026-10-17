import { CircleCheckIcon, CircleOffIcon, CircleXIcon, ClockIcon, UnplugIcon, type LucideIcon as Icon } from 'lucide-react'
import { Badge, type BadgeTint } from '@/components/ui/badge'
import type { HostStatus } from '../types/host.types'
import { HOST_STATUS_LABELS } from '../utils/status'

// `offline` (Sin conexion) es muted como `disabled`: es normal, no cuenta como falla.
const statusStyles: Record<HostStatus, { Icon: Icon; tint: BadgeTint }> = {
  up: { Icon: CircleCheckIcon, tint: 'ok' },
  down: { Icon: CircleXIcon, tint: 'danger' },
  offline: { Icon: UnplugIcon, tint: 'muted' },
  pending: { Icon: ClockIcon, tint: 'info' },
  disabled: { Icon: CircleOffIcon, tint: 'muted' },
}

export function HostStatusBadge({ status }: { status: HostStatus }) {
  const { Icon, tint } = statusStyles[status]
  return (
    <Badge Icon={Icon} tint={tint}>
      {HOST_STATUS_LABELS[status]}
    </Badge>
  )
}
