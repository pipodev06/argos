import type { ComponentType } from 'react'
import { IconAlertTriangle, IconCircleCheck, IconCircleX, IconHelpCircle } from '@tabler/icons-react'
import type { CheckStatus } from '../types/check.types'

interface StatusStyle {
  Icon: ComponentType<{ className?: string; stroke?: number; 'aria-hidden'?: boolean }>
  badge: string
  icon: string
  label: string
}

// Mismo patrón que HostStatusBadge: el color de estado va en el icono y el tinte; el texto usa `text`.
const styles: Record<CheckStatus, StatusStyle> = {
  up: { Icon: IconCircleCheck, badge: 'bg-ok/10', icon: 'text-ok', label: 'Activo' },
  down: { Icon: IconCircleX, badge: 'bg-danger/10', icon: 'text-danger', label: 'Caído' },
  warn: { Icon: IconAlertTriangle, badge: 'bg-warn/10', icon: 'text-warn', label: 'Advertencia' },
  unknown: { Icon: IconHelpCircle, badge: 'bg-text-muted/10', icon: 'text-text-muted', label: 'Desconocido' },
}

export function CheckStatusBadge({ status }: { status: CheckStatus }) {
  const { Icon, badge, icon, label } = styles[status]
  return (
    <span className={`inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-xs font-medium text-text ${badge}`}>
      <Icon className={`size-3.5 shrink-0 ${icon}`} stroke={1.5} aria-hidden />
      {label}
    </span>
  )
}
