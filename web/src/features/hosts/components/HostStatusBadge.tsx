import { IconCircleCheck, IconCircleOff, IconCircleX, IconClock, IconPlugConnectedX, type Icon } from '@tabler/icons-react'
import { Badge, type BadgeTint } from '@/components/ui/badge'
import type { HostStatus } from '../types/host.types'
import { HOST_STATUS_LABELS } from '../utils/status'

// `offline` (Sin conexion) es muted como `disabled`: es normal, no cuenta como falla.
const statusStyles: Record<HostStatus, { Icon: Icon; tint: BadgeTint }> = {
  up: { Icon: IconCircleCheck, tint: 'ok' },
  down: { Icon: IconCircleX, tint: 'danger' },
  offline: { Icon: IconPlugConnectedX, tint: 'muted' },
  pending: { Icon: IconClock, tint: 'info' },
  disabled: { Icon: IconCircleOff, tint: 'muted' },
}

export function HostStatusBadge({ status }: { status: HostStatus }) {
  const { Icon, tint } = statusStyles[status]
  return (
    <Badge Icon={Icon} tint={tint}>
      {HOST_STATUS_LABELS[status]}
    </Badge>
  )
}
