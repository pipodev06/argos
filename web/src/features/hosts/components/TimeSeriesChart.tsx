import { CartesianGrid, Line, LineChart, ReferenceLine, XAxis, YAxis } from 'recharts'
import { ChartContainer, ChartTooltip, ChartTooltipContent } from '@/components/ui/chart'
import type { MetricRange } from '../types/host.types'
import {
  buildTicks,
  formatTick,
  formatTooltipLabel,
  isolatedTimes,
  type ChartRow,
  type ChartSeries,
  type SeriesStyle,
} from '../utils/chart'

interface TimeSeriesChartProps {
  rows: ChartRow[]
  range: MetricRange
  /** Ventana visible [startMs, endMs]: fija el eje X aunque el host lleve rato sin reportar. */
  startMs: number
  endMs: number
  tickMs: number
  series: readonly ChartSeries[]
  yDomain: [number, number | ((dataMax: number) => number)]
  yTicks?: number[]
  yFormatter: (value: number) => string
  /** Linea horizontal de referencia (p. ej. memoria total). */
  reference?: SeriesStyle & { y: number }
}

const AXIS_TICK = { fill: 'var(--text-muted)', fontSize: 12 }

/** Grafica de lineas sobre eje de tiempo real; los huecos (valores null) cortan la linea. */
export function TimeSeriesChart({
  rows,
  range,
  startMs,
  endMs,
  tickMs,
  series,
  yDomain,
  yTicks,
  yFormatter,
  reference,
}: TimeSeriesChartProps) {
  return (
    <ChartContainer>
      <LineChart
        data={rows}
        margin={{ top: 8, right: 12, bottom: 0, left: 0 }}
        accessibilityLayer={false}
      >
        <CartesianGrid vertical={false} stroke="var(--border)" />
        <XAxis
          dataKey="t"
          type="number"
          domain={[startMs, endMs]}
          ticks={buildTicks(startMs, endMs, tickMs)}
          tickFormatter={(ms: number) => formatTick(ms, range)}
          tick={AXIS_TICK}
          stroke="var(--border)"
          tickLine={false}
          minTickGap={16}
        />
        <YAxis
          width={68}
          domain={yDomain}
          ticks={yTicks}
          tickFormatter={yFormatter}
          tick={AXIS_TICK}
          tickLine={false}
          axisLine={false}
        />
        <ChartTooltip
          isAnimationActive={false}
          cursor={{ stroke: 'var(--text-muted)', strokeDasharray: '3 3' }}
          content={
            <ChartTooltipContent
              labelFormatter={(label) => formatTooltipLabel(Number(label), range)}
              valueFormatter={yFormatter}
            />
          }
        />
        {reference && (
          <ReferenceLine
            y={reference.y}
            stroke={reference.color}
            strokeWidth={1.5}
            strokeDasharray={reference.dash}
          />
        )}
        {series.map((s) => {
          const isolated = isolatedTimes(rows, s.key)
          return (
            <Line
              key={s.key}
              type="linear"
              dataKey={s.key}
              name={s.name}
              stroke={s.color}
              strokeWidth={2}
              strokeDasharray={s.dash}
              connectNulls={false}
              isAnimationActive={false}
              activeDot={{ r: 4, fill: s.color, stroke: 'var(--surface)' }}
              // Un punto aislado (vecinos en hueco) no dibuja linea: se marca con un circulo.
              dot={({ cx, cy, index, payload }: DotProps) =>
                payload?.t !== undefined && isolated.has(payload.t) ? (
                  <circle key={index} cx={cx} cy={cy} r={3} fill={s.color} />
                ) : (
                  <g key={index} />
                )
              }
            />
          )
        })}
      </LineChart>
    </ChartContainer>
  )
}

interface DotProps {
  cx?: number
  cy?: number
  index?: number
  payload?: { t?: number }
}
