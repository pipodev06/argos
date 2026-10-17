import { IconAlertTriangle, IconCircleCheck, IconLock, type Icon } from '@tabler/icons-react'
import { Badge, type BadgeTint } from '@/components/ui/badge'
import type { AuditOutcome } from '../types/audit.types'
import { OUTCOME_LABELS } from '../utils/labels'

const STYLES: Record<AuditOutcome, { Icon: Icon; tint: BadgeTint }> = {
  ok: { Icon: IconCircleCheck, tint: 'ok' },
  fail: { Icon: IconAlertTriangle, tint: 'warn' },
  denied: { Icon: IconLock, tint: 'danger' },
}

export function OutcomeBadge({ outcome }: { outcome: AuditOutcome }) {
  const { Icon, tint } = STYLES[outcome]
  return (
    <Badge Icon={Icon} tint={tint}>
      {OUTCOME_LABELS[outcome]}
    </Badge>
  )
}
