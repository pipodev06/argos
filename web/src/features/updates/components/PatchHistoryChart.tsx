import { CartesianGrid, Line, LineChart, XAxis, YAxis } from 'recharts'
import { ChartContainer, ChartTooltip, ChartTooltipContent } from '@/components/ui/chart'
import { MetricChartCard } from '@/features/hosts/components/MetricChartCard'
import type { SeriesStyle } from '@/features/hosts/utils/chart'
import { formatDate, formatDateTime } from '@/features/hosts/utils/format'
import { buildPatchRows, describePatchRows, type PatchRow } from '../utils/patchChart'
import type { UpdatesPoint } from '../types/updates.types'

const TOTAL: SeriesStyle = { name: 'Pendientes', color: 'var(--chart-1)' }
const SECURITY: SeriesStyle = { name: 'De seguridad', color: 'var(--chart-2)', dash: '6 4' }
const AXIS_TICK = { fill: 'var(--text-muted)', fontSize: 12 }

interface DotProps {
  cx?: number
  cy?: number
  index?: number
  payload?: PatchRow
}

/** Punto de la serie; con reinicio pendiente lleva además un anillo (no depende del color). */
function makeDot(color: string, marksReboot: boolean) {
  return function Dot({ cx, cy, index, payload }: DotProps) {
    if (cx === undefined || cy === undefined) return <g key={index} />
    return (
      <g key={index}>
        <circle cx={cx} cy={cy} r={2.5} fill={color} />
        {marksReboot && payload?.reboot && <circle cx={cx} cy={cy} r={6} fill="none" stroke="var(--text)" strokeWidth={1.5} />}
      </g>
    )
  }
}

const totalDot = makeDot(TOTAL.color, true)
const securityDot = makeDot(SECURITY.color, false)

interface PatchHistoryChartProps {
  history: readonly UpdatesPoint[] | undefined
}

/** Historial de parches: pendientes y de seguridad (línea / punteada); círculo hueco = reinicio pendiente. */
export function PatchHistoryChart({ history }: PatchHistoryChartProps) {
  const rows = history ? buildPatchRows(history) : []
  const status = !history ? 'loading' : rows.length === 0 ? 'empty' : 'ready'
  return (
    <div className="flex flex-col gap-1">
    <MetricChartCard
      title="Evolución de parches"
      legend={[TOTAL, SECURITY]}
      status={status}
      description={describePatchRows(rows)}
    >
      <ChartContainer>
        <LineChart data={rows} margin={{ top: 8, right: 12, bottom: 0, left: 0 }} accessibilityLayer={false}>
          <CartesianGrid vertical={false} stroke="var(--border)" />
          <XAxis
            dataKey="t"
            type="number"
            scale="time"
            domain={['dataMin', 'dataMax']}
            tickFormatter={(ms: number) => formatDate(ms)}
            tick={AXIS_TICK}
            stroke="var(--border)"
            tickLine={false}
            minTickGap={24}
          />
          <YAxis width={40} allowDecimals={false} domain={[0, (max: number) => Math.max(max, 1)]} tick={AXIS_TICK} tickLine={false} axisLine={false} />
          <ChartTooltip
            isAnimationActive={false}
            cursor={{ stroke: 'var(--text-muted)', strokeDasharray: '3 3' }}
            content={<ChartTooltipContent labelFormatter={(l) => formatDateTime(Number(l))} valueFormatter={(v) => String(v)} />}
          />
          <Line type="linear" dataKey="total" name={TOTAL.name} stroke={TOTAL.color} strokeWidth={2} connectNulls={false} isAnimationActive={false} dot={totalDot} activeDot={{ r: 4, fill: TOTAL.color, stroke: 'var(--surface)' }} />
          <Line type="linear" dataKey="security" name={SECURITY.name} stroke={SECURITY.color} strokeWidth={2} strokeDasharray={SECURITY.dash} connectNulls={false} isAnimationActive={false} dot={securityDot} activeDot={{ r: 4, fill: SECURITY.color, stroke: 'var(--surface)' }} />
        </LineChart>
      </ChartContainer>
    </MetricChartCard>
    <p className="text-xs text-text-muted">Círculo alrededor del punto: reinicio pendiente. La tabla de abajo tiene los mismos datos.</p>
    </div>
  )
}
