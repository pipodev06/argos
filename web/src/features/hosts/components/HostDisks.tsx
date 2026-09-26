import { useState } from 'react'
import { UsageBar } from '@/components/ui/usage-bar'
import { Button } from '@/components/ui/button'
import type { HostDisk, PhysicalDisk } from '../types/host.types'
import { diskPct, groupDisks, physicalDiskDetail, physicalDiskLabel, sortDisks } from '../utils/detail'
import { formatCapacity } from '../utils/format'

const FOLD_AFTER = 6

function VolumeRow({ d }: { d: HostDisk }) {
  return (
    <li className="flex flex-wrap items-center gap-x-4 gap-y-1 border-b border-border px-3 py-2 last:border-b-0">
      <span className="w-56 max-w-full min-w-0 break-all text-sm" title={d.mount}>
        {d.mount}
      </span>
      <span className="w-36 text-xs text-text-muted tabular-nums">
        {formatCapacity(d.used_mb)} / {formatCapacity(d.total_mb)}
      </span>
      <div className="min-w-56 flex-1">
        <UsageBar value={diskPct(d)} label={`Uso del disco ${d.mount}`} />
      </div>
    </li>
  )
}

/** Discos del host: agrupados por disco físico cuando el agente lo reporta; si no, la lista plana de volúmenes. */
export function HostDisks({ disks, physical }: { disks: readonly HostDisk[]; physical?: readonly PhysicalDisk[] | null }) {
  const [expanded, setExpanded] = useState(false)
  const { groups, loose } = groupDisks(disks, physical ?? null)
  const grouped = groups.length > 0

  // Sin discos fisicos conocidos: la lista plana de siempre (con "Mostrar mas" si son muchos).
  const sorted = sortDisks(disks)
  const shown = expanded ? sorted : sorted.slice(0, FOLD_AFTER)
  const hidden = sorted.length - shown.length

  return (
    <section aria-labelledby="disks-title" className="flex flex-col gap-2">
      <h2 id="disks-title" className="text-sm font-semibold">
        Discos
      </h2>
      {disks.length === 0 && !grouped ? (
        <p className="text-sm text-text-muted">Sin datos de discos.</p>
      ) : grouped ? (
        <div className="flex flex-col gap-3">
          {groups.map(({ disk, volumes }) => (
            <div key={disk.id} className="flex flex-col overflow-hidden rounded-md border border-border bg-surface">
              <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1 border-b border-border px-3 py-2">
                <h3 className="text-sm font-semibold">{physicalDiskLabel(disk)}</h3>
                <span className="text-xs text-text-muted">{physicalDiskDetail(disk) || 'Modelo desconocido'}</span>
                {disk.size_mb !== null && <span className="text-xs text-text-muted tabular-nums">{formatCapacity(disk.size_mb)}</span>}
              </div>
              {volumes.length === 0 ? (
                <p className="px-3 py-2 text-xs text-text-muted">Sin volúmenes reportados.</p>
              ) : (
                <ul>
                  {volumes.map((d) => (
                    <VolumeRow key={d.mount} d={d} />
                  ))}
                </ul>
              )}
            </div>
          ))}
          {loose.length > 0 && (
            <div className="flex flex-col overflow-hidden rounded-md border border-border bg-surface">
              <div className="border-b border-border px-3 py-2">
                <h3 className="text-sm font-semibold">Otros volúmenes</h3>
              </div>
              <ul>
                {loose.map((d) => (
                  <VolumeRow key={d.mount} d={d} />
                ))}
              </ul>
            </div>
          )}
        </div>
      ) : (
        <ul className="flex flex-col overflow-hidden rounded-md border border-border bg-surface">
          {shown.map((d) => (
            <VolumeRow key={d.mount} d={d} />
          ))}
        </ul>
      )}
      {!grouped && sorted.length > FOLD_AFTER && (
        <Button variant="ghost" size="sm" className="w-fit" onClick={() => setExpanded((e) => !e)}>
          {expanded ? 'Mostrar menos' : `Mostrar ${hidden} más`}
        </Button>
      )}
    </section>
  )
}
