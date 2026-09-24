import { useState } from 'react'
import { UsageBar } from '@/components/ui/usage-bar'
import { Button } from '@/components/ui/button'
import type { HostDisk } from '../types/host.types'
import { diskPct, sortDisks } from '../utils/detail'
import { formatCapacity } from '../utils/format'

const FOLD_AFTER = 6

export function HostDisks({ disks }: { disks: readonly HostDisk[] }) {
  const [expanded, setExpanded] = useState(false)
  const sorted = sortDisks(disks)
  const shown = expanded ? sorted : sorted.slice(0, FOLD_AFTER)
  const hidden = sorted.length - shown.length

  return (
    <section aria-labelledby="disks-title" className="flex flex-col gap-2">
      <h2 id="disks-title" className="text-sm font-semibold">
        Discos
      </h2>
      {sorted.length === 0 ? (
        <p className="text-sm text-text-muted">Sin datos de discos.</p>
      ) : (
        <ul className="flex flex-col overflow-hidden rounded-md border border-border bg-surface">
          {shown.map((d) => (
            <li key={d.mount} className="flex flex-wrap items-center gap-x-4 gap-y-1 border-b border-border px-3 py-2 last:border-b-0">
              <span className="w-32 min-w-0 truncate text-sm">{d.mount}</span>
              <span className="w-36 text-xs text-text-muted tabular-nums">
                {formatCapacity(d.used_mb)} / {formatCapacity(d.total_mb)}
              </span>
              <div className="min-w-56 flex-1">
                <UsageBar value={diskPct(d)} label={`Uso del disco ${d.mount}`} />
              </div>
            </li>
          ))}
        </ul>
      )}
      {sorted.length > FOLD_AFTER && (
        <Button variant="ghost" size="sm" className="w-fit" onClick={() => setExpanded((e) => !e)}>
          {expanded ? 'Mostrar menos' : `Mostrar ${hidden} más`}
        </Button>
      )}
    </section>
  )
}
