import { OctagonAlertIcon, TriangleAlertIcon, CircleCheckIcon, type LucideIcon as Icon } from 'lucide-react'
import type { ServiceLevel } from '../types/systemStatus.types'
import { LEVEL_LABELS } from '../utils/systemStatus'

// Estado = icono + palabra; el color solo refuerza el icono.
const ICONS: Record<ServiceLevel, { Icon: Icon; color: string }> = {
  ok: { Icon: CircleCheckIcon, color: 'text-ok' },
  warn: { Icon: TriangleAlertIcon, color: 'text-warn' },
  crit: { Icon: OctagonAlertIcon, color: 'text-danger' },
}

export function LevelBadge({ level }: { level: ServiceLevel }) {
  const { Icon: LevelIcon, color } = ICONS[level]
  return (
    <span className="inline-flex items-center gap-1.5">
      <LevelIcon className={`size-4 shrink-0 ${color}`} strokeWidth={1.5} aria-hidden />
      {LEVEL_LABELS[level]}
    </span>
  )
}
