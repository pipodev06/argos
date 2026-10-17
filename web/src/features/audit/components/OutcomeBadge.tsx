import { TriangleAlertIcon, CircleCheckIcon, LockIcon, type LucideIcon as Icon } from 'lucide-react'
import { Badge, type BadgeTint } from '@/components/ui/badge'
import type { AuditOutcome } from '../types/audit.types'
import { OUTCOME_LABELS } from '../utils/labels'

const STYLES: Record<AuditOutcome, { Icon: Icon; tint: BadgeTint }> = {
  ok: { Icon: CircleCheckIcon, tint: 'ok' },
  fail: { Icon: TriangleAlertIcon, tint: 'warn' },
  denied: { Icon: LockIcon, tint: 'danger' },
}

export function OutcomeBadge({ outcome }: { outcome: AuditOutcome }) {
  const { Icon, tint } = STYLES[outcome]
  return (
    <Badge Icon={Icon} tint={tint}>
      {OUTCOME_LABELS[outcome]}
    </Badge>
  )
}
