import { useState } from 'react'
import { SegmentedControl } from '@/components/ui/segmented-control'
import { useHostMetrics } from '../hooks/useHostMetrics'
import { useHostOutlet } from '../hooks/useHostOutlet'
import type { MetricRange } from '../types/host.types'
import { RANGES, rangeView } from '../utils/chart'
import { HostCharts } from './HostCharts'
import { HostHistoryPanel } from './HostHistoryPanel'

type MetricsView = MetricRange | 'custom'

const OPTIONS = [
  ...RANGES.map(({ value, label, ariaLabel }) => ({ value, label, ariaLabel })),
  { value: 'custom' as const, label: 'Personalizado', ariaLabel: 'Rango personalizado por fecha' },
]

/** Gráficas de un rango relativo; `useHostMetrics` (y su polling) solo existe mientras esta vista está montada. */
function LiveMetrics({ id, range }: { id: string; range: MetricRange }) {
  const metrics = useHostMetrics(id, range)
  return <HostCharts view={rangeView(range, metrics.updatedAt ?? 0)} metrics={metrics.data} error={metrics.error} />
}

/** Ruta `/hosts/:id/metricas`: la única que carga Recharts (lazy desde DashboardPage). */
export function HostMetricsTab() {
  const { hostId, detail } = useHostOutlet()
  const [range, setRange] = useState<MetricsView>('1h')
  return (
    <div className="flex flex-col gap-3">
      <div className="flex justify-end">
        <SegmentedControl aria-label="Rango de tiempo" options={OPTIONS} value={range} onChange={setRange} />
      </div>
      {range === 'custom' ? (
        <HostHistoryPanel id={hostId} hostname={detail.data?.host.hostname ?? ''} />
      ) : (
        <LiveMetrics id={hostId} range={range} />
      )}
    </div>
  )
}
