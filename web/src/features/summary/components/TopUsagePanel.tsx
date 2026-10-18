import { useState } from 'react'
import { ViewAction } from '@/components/data-table/RowActions'
import { SegmentedControl } from '@/components/ui/segmented-control'
import { UsageBar } from '@/components/ui/usage-bar'
import type { Summary, TopHost } from '../types/summary.types'

type Metric = 'cpu' | 'mem' | 'disk'

const OPTIONS: { value: Metric; label: string }[] = [
  { value: 'cpu', label: 'CPU' },
  { value: 'mem', label: 'Memoria' },
  { value: 'disk', label: 'Disco' },
]

const NAMES: Record<Metric, string> = { cpu: 'CPU', mem: 'memoria', disk: 'disco' }

function rowsFor(summary: Summary, metric: Metric): TopHost[] {
  const list = metric === 'cpu' ? summary.top_cpu : metric === 'mem' ? summary.top_mem : summary.top_disk
  return list.slice(0, 5)
}

export function TopUsagePanel({ summary }: { summary: Summary }) {
  const [metric, setMetric] = useState<Metric>('cpu')
  const rows = rowsFor(summary, metric)

  return (
    <section aria-labelledby="top-title" className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 id="top-title" className="text-sm font-semibold">
          Mayor uso
        </h2>
        <SegmentedControl aria-label="Métrica" options={OPTIONS} value={metric} onChange={setMetric} />
      </div>
      {rows.length === 0 ? (
        <p className="rounded-md border border-border bg-surface px-3 py-3 text-sm text-text-muted">
          Sin datos de hosts activos.
        </p>
      ) : (
        <ul className="flex flex-col overflow-hidden rounded-md border border-border bg-surface">
          {rows.map((h) => (
            <li key={h.id} className="flex flex-wrap items-center gap-x-4 gap-y-1 border-b border-border px-3 py-2 last:border-b-0">
              <span className="w-40 min-w-0 truncate text-sm font-semibold">{h.hostname}</span>
              {h.mount && <span className="text-sm text-text-muted">{h.mount}</span>}
              <div className="min-w-48 flex-1">
                <UsageBar value={h.value} label={`Uso de ${NAMES[metric]} de ${h.hostname}`} />
              </div>
              <ViewAction to={`/hosts/${h.id}`} label={h.hostname} />
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
