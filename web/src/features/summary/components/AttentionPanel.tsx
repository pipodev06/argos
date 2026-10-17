import { CircleCheckIcon, CircleXIcon } from 'lucide-react'
import { Link } from 'react-router'
import { formatElapsed } from '@/features/hosts/utils/format'
import type { RecentDown } from '../types/summary.types'

export function AttentionPanel({ items, now }: { items: RecentDown[]; now: number }) {
  return (
    <section aria-labelledby="attention-title" className="flex flex-col gap-2">
      <div className="flex items-center justify-between gap-2">
        <h2 id="attention-title" className="text-sm font-semibold">
          Requieren atención
        </h2>
        <Link to="/hosts?status=down" className="rounded-md text-xs text-text-muted outline-none hover:text-text hover:underline focus-visible:ring-2 focus-visible:ring-ring">
          Ver todos los caídos →
        </Link>
      </div>
      {items.length === 0 ? (
        <p className="flex items-center gap-2 rounded-md border border-border bg-surface px-3 py-3 text-sm">
          <CircleCheckIcon className="size-4 shrink-0 text-ok" strokeWidth={1.5} aria-hidden />
          Ningún host caído
        </p>
      ) : (
        <ul className="flex flex-col overflow-hidden rounded-md border border-border bg-surface">
          {items.slice(0, 5).map((h) => (
            <li key={h.id} className="border-b border-border last:border-b-0">
              <Link
                to={`/hosts/${h.id}`}
                className="flex min-h-9 items-center gap-2 px-3 py-1.5 text-sm outline-none hover:bg-muted/50 focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
              >
                <CircleXIcon className="size-4 shrink-0 text-danger" strokeWidth={1.5} aria-label="Caído" />
                <span className="min-w-0 truncate font-semibold">{h.hostname}</span>
                <span className="hidden truncate text-text-muted sm:inline">{h.group?.name ?? 'Sin grupo'}</span>
                <span className="ml-auto shrink-0 text-sm text-text-muted tabular-nums">
                  caído hace {formatElapsed(h.status_changed_at, now)}
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
