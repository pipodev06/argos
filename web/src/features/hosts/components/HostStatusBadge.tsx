import type { ComponentType } from 'react'
import {
  IconCircleCheck,
  IconCircleOff,
  IconCircleX,
  IconClock,
  IconPlugConnectedX,
} from '@tabler/icons-react'
import type { HostStatus } from '../types/host.types'
import { HOST_STATUS_LABELS } from '../utils/status'

interface StatusStyle {
  Icon: ComponentType<{ className?: string; stroke?: number; 'aria-hidden'?: boolean }>
  badge: string
  icon: string
}

// El color de estado va en el icono y el tinte de fondo; el texto siempre usa `text`.
// `offline` (Sin conexion) es muted como `disabled`: es normal, no cuenta como falla.
const statusStyles: Record<HostStatus, StatusStyle> = {
  up: { Icon: IconCircleCheck, badge: 'bg-ok/10', icon: 'text-ok' },
  down: { Icon: IconCircleX, badge: 'bg-danger/10', icon: 'text-danger' },
  offline: { Icon: IconPlugConnectedX, badge: 'bg-text-muted/10', icon: 'text-text-muted' },
  pending: { Icon: IconClock, badge: 'bg-info/10', icon: 'text-info' },
  disabled: { Icon: IconCircleOff, badge: 'bg-text-muted/10', icon: 'text-text-muted' },
}

export function HostStatusBadge({ status }: { status: HostStatus }) {
  const { Icon, badge, icon } = statusStyles[status]
  return (
    <span
      className={`inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-xs font-medium text-text ${badge}`}
    >
      <Icon className={`size-3.5 shrink-0 ${icon}`} stroke={1.5} aria-hidden />
      {HOST_STATUS_LABELS[status]}
    </span>
  )
}
