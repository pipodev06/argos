import { IconAlertTriangle, IconCircleCheck, IconLock, type Icon } from '@tabler/icons-react'
import type { AuditOutcome } from '../types/audit.types'
import { OUTCOME_LABELS } from '../utils/labels'

// Estado solo en icono y tinte de fondo; el texto va en `text` (nunca en color de estado).
const STYLES: Record<AuditOutcome, { Icon: Icon; icon: string; bg: string }> = {
  ok: { Icon: IconCircleCheck, icon: 'text-ok', bg: 'bg-ok/10' },
  fail: { Icon: IconAlertTriangle, icon: 'text-warn', bg: 'bg-warn/10' },
  denied: { Icon: IconLock, icon: 'text-danger', bg: 'bg-danger/10' },
}

export function OutcomeBadge({ outcome }: { outcome: AuditOutcome }) {
  const { Icon, icon, bg } = STYLES[outcome]
  return (
    <span className={`inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-xs font-medium text-text ${bg}`}>
      <Icon className={`size-3.5 shrink-0 ${icon}`} stroke={1.5} aria-hidden />
      {OUTCOME_LABELS[outcome]}
    </span>
  )
}
