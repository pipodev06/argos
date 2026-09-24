import { IconArrowRight } from '@tabler/icons-react'
import { HostStatusBadge } from '@/features/hosts/components/HostStatusBadge'
import { formatFullDate } from '@/features/hosts/utils/format'
import type { HostEvent } from '../types/alert.types'
import { HOST_EVENT_SOURCE_LABELS } from '../utils/format'

/** Transiciones de estado de un host (más recientes primero): de → a, hora y origen. */
export function HostEventsList({ events }: { events: HostEvent[] }) {
  if (events.length === 0) return <p className="text-sm text-text-muted">Sin cambios de estado registrados.</p>
  return (
    <ul className="divide-y divide-border rounded-md border border-border bg-surface">
      {events.map((e) => (
        <li key={e.id} className="flex flex-wrap items-center gap-x-4 gap-y-1 px-3 py-2 text-sm">
          <span className="flex items-center gap-2">
            <HostStatusBadge status={e.from_status} />
            <IconArrowRight className="size-4 shrink-0 text-text-muted" stroke={1.5} aria-label="a" />
            <HostStatusBadge status={e.to_status} />
          </span>
          <span className="tabular-nums">{formatFullDate(e.at)}</span>
          <span className="text-xs text-text-muted">{HOST_EVENT_SOURCE_LABELS[e.source]}</span>
        </li>
      ))}
    </ul>
  )
}
