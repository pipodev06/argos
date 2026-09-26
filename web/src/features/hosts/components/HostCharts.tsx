import { useMemo } from 'react'
import { InlineError } from '@/components/ui/inline-error'
import type { MetricSample } from '../types/host.types'
import {
  buildRows,
  describeSeries,
  summarize,
  type ChartSeries,
  type ChartView,
  type SeriesKey,
  type SeriesStyle,
} from '../utils/chart'
import { formatBps, formatMemory, formatNumber, formatPercent } from '../utils/format'
import { MetricChartCard, type ChartStatus } from './MetricChartCard'
import { TimeSeriesChart } from './TimeSeriesChart'

/** Datos que necesitan las graficas: los devuelven tanto `/metrics` como `/history`. */
interface ChartData {
  step_seconds: number
  points: MetricSample[]
}

interface HostChartsProps {
  /** Ventana visible, ejes y frase de la descripcion accesible. */
  view: ChartView
  metrics: ChartData | null
  error: string | null
}

// Paleta categorica (index.css): una metrica por grafica; la 2a serie de Red se distingue tambien por trazo.
const CPU: ChartSeries = { key: 'cpu', name: 'CPU', color: 'var(--chart-1)' }
const MEM: ChartSeries = { key: 'memUsed', name: 'Usada', color: 'var(--chart-3)' }
const RX: ChartSeries = { key: 'rx', name: 'Bajada', color: 'var(--chart-1)' }
const TX: ChartSeries = { key: 'tx', name: 'Subida', color: 'var(--chart-2)', dash: '6 4' }
const TOTAL_STYLE: SeriesStyle = { name: 'Total', color: 'var(--text-muted)', dash: '6 3 1 3' }

const SWAP: ChartSeries = { key: 'swapUsed', name: 'Usada', color: 'var(--chart-4)' }
const LOAD1: ChartSeries = { key: 'load1', name: '1 min', color: 'var(--chart-1)' }
const LOAD5: ChartSeries = { key: 'load5', name: '5 min', color: 'var(--chart-2)', dash: '6 4' }
const LOAD15: ChartSeries = { key: 'load15', name: '15 min', color: 'var(--chart-3)', dash: '6 3 1 3' }
const BATTERY: ChartSeries = { key: 'battery', name: 'Batería', color: 'var(--chart-5)' }
const MIN_LOAD_MAX = 1
const formatLoad = (v: number) => formatNumber(v, 2)

const CPU_TICKS = [0, 25, 50, 75, 100]
const MIN_NET_MAX_BPS = 1000

export function HostCharts({ view, metrics, error }: HostChartsProps) {
  const rows = useMemo(
    () => (metrics ? buildRows(metrics.points, metrics.step_seconds) : []),
    [metrics],
  )

  const memTotal = metrics ? Math.max(0, ...metrics.points.map((p) => p.mem_total_mb)) : 0
  const swapTotal = metrics ? Math.max(0, ...metrics.points.map((p) => p.swap_total_mb ?? 0)) : 0
  // Sin dato en todo el rango (sin swap, Windows sin carga, sin bateria): la grafica se oculta.
  const showSwap = swapTotal > 0
  const showLoad = (['load1', 'load5', 'load15'] as const).some((k) => summarize(rows, k) !== null)
  const showBattery = summarize(rows, 'battery') !== null

  function statusFor(...keys: SeriesKey[]): ChartStatus {
    if (metrics === null) return error ? 'error' : 'loading'
    return keys.some((k) => summarize(rows, k) !== null) ? 'ready' : 'empty'
  }

  const timeProps = { rows, view }
  const cardErrorMessage = 'No se pudieron cargar las métricas.'

  return (
    <div className="flex flex-col gap-4">
      {error && <InlineError>{error}</InlineError>}

      <MetricChartCard
        title="CPU"
        legend={[CPU]}
        status={statusFor('cpu')}
        errorMessage={cardErrorMessage}
        description={`Uso de CPU ${view.phrase}. ${describeSeries('CPU', summarize(rows, 'cpu'), formatPercent)}`}
      >
        <TimeSeriesChart
          {...timeProps}
          series={[CPU]}
          yDomain={[0, 100]}
          yTicks={CPU_TICKS}
          yFormatter={(v) => `${v}%`}
        />
      </MetricChartCard>

      <MetricChartCard
        title="Memoria"
        legend={[MEM, TOTAL_STYLE]}
        status={statusFor('memUsed')}
        errorMessage={cardErrorMessage}
        description={`Memoria usada ${view.phrase}, de ${formatMemory(memTotal)} en total. ${describeSeries('Usada', summarize(rows, 'memUsed'), formatMemory)}`}
      >
        <TimeSeriesChart
          {...timeProps}
          series={[MEM]}
          // el dominio deja un margen sobre el total pa que la linea de referencia no quede recortada
          yDomain={[0, memTotal * 1.08]}
          yTicks={[0, 0.25, 0.5, 0.75, 1].map((f) => memTotal * f)}
          yFormatter={formatMemory}
          reference={{ ...TOTAL_STYLE, y: memTotal }}
        />
      </MetricChartCard>

      <MetricChartCard
        title="Red"
        legend={[RX, TX]}
        status={statusFor('rx', 'tx')}
        errorMessage={cardErrorMessage}
        description={`Tráfico de red ${view.phrase}. ${describeSeries('Bajada', summarize(rows, 'rx'), formatBps)}. ${describeSeries('Subida', summarize(rows, 'tx'), formatBps)}`}
      >
        <TimeSeriesChart
          {...timeProps}
          series={[RX, TX]}
          yDomain={[0, (dataMax) => Math.max(dataMax, MIN_NET_MAX_BPS)]}
          yFormatter={formatBps}
        />
      </MetricChartCard>

      {showSwap && (
        <MetricChartCard
          title="Swap"
          legend={[SWAP, TOTAL_STYLE]}
          status={statusFor('swapUsed')}
          errorMessage={cardErrorMessage}
          description={`Swap usada ${view.phrase}, de ${formatMemory(swapTotal)} en total. ${describeSeries('Usada', summarize(rows, 'swapUsed'), formatMemory)}`}
        >
          <TimeSeriesChart
            {...timeProps}
            series={[SWAP]}
            yDomain={[0, swapTotal * 1.08]}
            yTicks={[0, 0.25, 0.5, 0.75, 1].map((f) => swapTotal * f)}
            yFormatter={formatMemory}
            reference={{ ...TOTAL_STYLE, y: swapTotal }}
          />
        </MetricChartCard>
      )}

      {showLoad && (
        <MetricChartCard
          title="Carga"
          legend={[LOAD1, LOAD5, LOAD15]}
          status={statusFor('load1', 'load5', 'load15')}
          errorMessage={cardErrorMessage}
          description={`Carga del sistema ${view.phrase}. ${describeSeries('1 min', summarize(rows, 'load1'), formatLoad)}. ${describeSeries('5 min', summarize(rows, 'load5'), formatLoad)}. ${describeSeries('15 min', summarize(rows, 'load15'), formatLoad)}`}
        >
          <TimeSeriesChart
            {...timeProps}
            series={[LOAD1, LOAD5, LOAD15]}
            yDomain={[0, (dataMax) => Math.max(dataMax * 1.1, MIN_LOAD_MAX)]}
            yFormatter={formatLoad}
          />
        </MetricChartCard>
      )}

      {showBattery && (
        <MetricChartCard
          title="Batería"
          legend={[BATTERY]}
          status={statusFor('battery')}
          errorMessage={cardErrorMessage}
          description={`Batería ${view.phrase}. ${describeSeries('Batería', summarize(rows, 'battery'), formatPercent)}`}
        >
          <TimeSeriesChart
            {...timeProps}
            series={[BATTERY]}
            yDomain={[0, 100]}
            yTicks={CPU_TICKS}
            yFormatter={(v) => `${v}%`}
          />
        </MetricChartCard>
      )}
    </div>
  )
}
