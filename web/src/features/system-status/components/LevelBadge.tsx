import { IconAlertOctagon, IconAlertTriangle, IconCircleCheck, type Icon } from '@tabler/icons-react'
import type { ServiceLevel } from '../types/systemStatus.types'
import { LEVEL_LABELS } from '../utils/systemStatus'

// Estado = icono + palabra; el color solo refuerza el icono.
const ICONS: Record<ServiceLevel, { Icon: Icon; color: string }> = {
  ok: { Icon: IconCircleCheck, color: 'text-ok' },
  warn: { Icon: IconAlertTriangle, color: 'text-warn' },
  crit: { Icon: IconAlertOctagon, color: 'text-danger' },
}

export function LevelBadge({ level }: { level: ServiceLevel }) {
  const { Icon: LevelIcon, color } = ICONS[level]
  return (
    <span className="inline-flex items-center gap-1.5">
      <LevelIcon className={`size-4 shrink-0 ${color}`} stroke={1.5} aria-hidden />
      {LEVEL_LABELS[level]}
    </span>
  )
}
