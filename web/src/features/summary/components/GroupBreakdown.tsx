import { CircleXIcon } from 'lucide-react'
import { Link } from 'react-router'
import type { SummaryGroup } from '../types/summary.types'

export function GroupBreakdown({ groups }: { groups: SummaryGroup[] }) {
  return (
    <section aria-labelledby="groups-title" className="flex flex-col gap-2">
      <h2 id="groups-title" className="text-sm font-semibold">
        Por grupo
      </h2>
      {groups.length === 0 ? (
        <p className="rounded-md border border-border bg-surface px-3 py-3 text-sm text-text-muted">Sin grupos con hosts.</p>
      ) : (
        <ul className="flex flex-col overflow-hidden rounded-md border border-border bg-surface">
          {groups.map((g) => {
            const pct = g.total > 0 ? (g.up / g.total) * 100 : 0
            return (
              <li key={g.group_id ?? 'none'} className="border-b border-border last:border-b-0">
                <Link
                  to={`/hosts?group=${g.group_id ?? 'none'}`}
                  className="flex min-h-9 flex-col gap-1 px-3 py-2 text-sm outline-none hover:bg-muted/50 focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
                >
                  <span className="flex flex-wrap items-baseline justify-between gap-x-3">
                    <span className="min-w-0 truncate font-semibold">{g.name}</span>
                    <span className="flex items-center gap-3 text-xs tabular-nums">
                      {g.down > 0 && (
                        <span className="flex items-center gap-1">
                          <CircleXIcon className="size-3.5 text-danger" strokeWidth={1.5} aria-hidden />
                          {g.down} {g.down === 1 ? 'caído' : 'caídos'}
                        </span>
                      )}
                      <span className="text-text-muted">
                        {g.up} de {g.total} activos
                      </span>
                    </span>
                  </span>
                  <span aria-hidden className="h-1.5 w-full overflow-hidden rounded-md bg-border">
                    <span className="block h-full bg-ok" style={{ width: `${pct}%` }} />
                  </span>
                </Link>
              </li>
            )
          })}
        </ul>
      )}
    </section>
  )
}
