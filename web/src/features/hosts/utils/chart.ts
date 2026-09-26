import type { MetricRange, MetricSample } from '../types/host.types'
import { formatClock, formatDate, formatDateHour, formatDateTime, formatDay } from './format'

interface RangeConfig {
  value: MetricRange
  label: string
  /** Nombre accesible completo del boton del selector. */
  ariaLabel: string
  /** Frase para la descripcion accesible ("en la ultima hora"). */
  phrase: string
  windowMs: number
  /** Separacion entre marcas del eje X. */
  tickMs: number
  /** Milisegundos entre refrescos automaticos (los rangos largos vienen de resumenes por hora). */
  refreshMs: number
}

const MINUTE = 60_000
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR
const REFRESH_SHORT_MS = 30_000
const REFRESH_LONG_MS = 5 * MINUTE

export const RANGES: readonly RangeConfig[] = [
  {
    value: '1h',
    label: '1h',
    ariaLabel: 'Última hora',
    phrase: 'en la última hora',
    windowMs: HOUR,
    tickMs: 10 * MINUTE,
    refreshMs: REFRESH_SHORT_MS,
  },
  {
    value: '6h',
    label: '6h',
    ariaLabel: 'Últimas 6 horas',
    phrase: 'en las últimas 6 horas',
    windowMs: 6 * HOUR,
    tickMs: HOUR,
    refreshMs: REFRESH_SHORT_MS,
  },
  {
    value: '24h',
    label: '24h',
    ariaLabel: 'Últimas 24 horas',
    phrase: 'en las últimas 24 horas',
    windowMs: DAY,
    tickMs: 4 * HOUR,
    refreshMs: REFRESH_SHORT_MS,
  },
  {
    value: '7d',
    label: '7d',
    ariaLabel: 'Últimos 7 días',
    phrase: 'en los últimos 7 días',
    windowMs: 7 * DAY,
    tickMs: DAY,
    refreshMs: REFRESH_SHORT_MS,
  },
  {
    value: '30d',
    label: '30d',
    ariaLabel: 'Últimos 30 días',
    phrase: 'en los últimos 30 días',
    windowMs: 30 * DAY,
    tickMs: 5 * DAY,
    refreshMs: REFRESH_LONG_MS,
  },
  {
    value: '90d',
    label: '90d',
    ariaLabel: 'Últimos 90 días',
    phrase: 'en los últimos 90 días',
    windowMs: 90 * DAY,
    tickMs: 15 * DAY,
    refreshMs: REFRESH_LONG_MS,
  },
  {
    value: '1y',
    label: '1a',
    ariaLabel: 'Último año',
    phrase: 'en el último año',
    windowMs: 365 * DAY,
    tickMs: 60 * DAY,
    refreshMs: REFRESH_LONG_MS,
  },
]

export function getRange(range: MetricRange): RangeConfig {
  return RANGES.find((r) => r.value === range) ?? RANGES[0]
}

/** Ventana y formato de ejes de una grafica: relativa a "ahora" (rangos) o fija (historial por fecha). */
export interface ChartView {
  startMs: number
  endMs: number
  tickMs: number
  /** Frase para la descripcion accesible ("en la ultima hora"). */
  phrase: string
  formatTick: (ms: number) => string
  formatTooltip: (ms: number) => string
}

/** Vista de un rango relativo que termina en `endMs`. */
export function rangeView(range: MetricRange, endMs: number): ChartView {
  const config = getRange(range)
  return {
    startMs: endMs - config.windowMs,
    endMs,
    tickMs: config.tickMs,
    phrase: config.phrase,
    formatTick: (ms) => formatTick(ms, range),
    formatTooltip: (ms) => formatTooltipLabel(ms, range),
  }
}

/** Fila del grafico: `t` en ms epoch; una serie en null = sin dato (hueco). */
export interface ChartRow {
  t: number
  cpu: number | null
  memUsed: number | null
  rx: number | null
  tx: number | null
  swapUsed: number | null
  load1: number | null
  load5: number | null
  load15: number | null
  battery: number | null
}

export type SeriesKey = 'cpu' | 'memUsed' | 'rx' | 'tx' | 'swapUsed' | 'load1' | 'load5' | 'load15' | 'battery'

const EMPTY_VALUES: Omit<ChartRow, 't'> = {
  cpu: null,
  memUsed: null,
  rx: null,
  tx: null,
  swapUsed: null,
  load1: null,
  load5: null,
  load15: null,
  battery: null,
}

/** Estilo de una serie: color (`var(--chart-N)`) + patron de trazo (vacio = solida). */
export interface SeriesStyle {
  name: string
  color: string
  dash?: string
}

export interface ChartSeries extends SeriesStyle {
  key: SeriesKey
}

/**
 * Convierte los puntos del servidor en filas ordenadas por tiempo. Solo hay puntos donde hubo datos,
 * asi que si entre dos puntos consecutivos falta al menos un bucket (> 1,5 pasos) se inserta una fila
 * vacia en el bucket siguiente: con `connectNulls={false}` eso corta la linea (hueco, no interpolacion).
 */
export function buildRows(points: readonly MetricSample[], stepSeconds: number): ChartRow[] {
  const stepMs = stepSeconds * 1000
  const rows: ChartRow[] = []
  for (const p of points) {
    const t = new Date(p.ts).getTime()
    if (Number.isNaN(t)) continue
    const prev = rows.at(-1)
    if (prev && stepMs > 0 && t - prev.t > stepMs * 1.5) {
      rows.push({ t: prev.t + stepMs, ...EMPTY_VALUES })
    }
    rows.push({
      t,
      cpu: p.cpu_pct,
      memUsed: p.mem_used_mb,
      rx: p.net_rx_bps,
      tx: p.net_tx_bps,
      swapUsed: p.swap_used_mb ?? null,
      load1: p.load1 ?? null,
      load5: p.load5 ?? null,
      load15: p.load15 ?? null,
      battery: p.battery_pct ?? null,
    })
  }
  return rows
}

/** Marcas del eje X alineadas a la hora local (cada `tickMs` dentro de [start, end]). */
export function buildTicks(startMs: number, endMs: number, tickMs: number): number[] {
  const offset = new Date(startMs).getTimezoneOffset() * MINUTE
  const first = Math.ceil((startMs - offset) / tickMs) * tickMs + offset
  const ticks: number[] = []
  for (let t = first; t <= endMs; t += tickMs) ticks.push(t)
  return ticks
}

/**
 * Etiqueta de una marca del eje X. Rangos cortos: hora; 7d: dia de la semana; 30d/90d/1y: fecha
 * (las marcas caen en medianoche local, asi que la hora no aporta; 1y agrega el ano).
 */
function formatTick(ms: number, range: MetricRange): string {
  switch (range) {
    case '7d':
      return formatDay(ms)
    case '30d':
    case '90d':
      return formatDate(ms)
    case '1y':
      return formatDate(ms, true)
    default:
      return formatClock(ms)
  }
}

/** Titulo del tooltip: 30d fecha + hora (pasos de 1 h); 90d y 1y solo fecha; el resto, fecha y hora con segundos. */
function formatTooltipLabel(ms: number, range: MetricRange): string {
  switch (range) {
    case '30d':
      return formatDateHour(ms)
    case '90d':
      return formatDate(ms)
    case '1y':
      return formatDate(ms, true)
    default:
      return formatDateTime(ms)
  }
}

/**
 * Tiempos de los puntos que quedan aislados en una serie (vecinos nulos o extremo): con solo linea
 * serian invisibles, asi que se les dibuja un punto.
 */
export function isolatedTimes(rows: readonly ChartRow[], key: SeriesKey): Set<number> {
  const out = new Set<number>()
  rows.forEach((row, i) => {
    if (row[key] === null) return
    if (rows[i - 1]?.[key] == null && rows[i + 1]?.[key] == null) out.add(row.t)
  })
  return out
}

export interface SeriesSummary {
  last: number
  min: number
  max: number
}

/** Ultimo, minimo y maximo de una serie ignorando huecos; null si no hay ningun valor. */
export function summarize(rows: readonly ChartRow[], key: SeriesKey): SeriesSummary | null {
  let last: number | null = null
  let min = Infinity
  let max = -Infinity
  for (const row of rows) {
    const v = row[key]
    if (v === null) continue
    last = v
    if (v < min) min = v
    if (v > max) max = v
  }
  return last === null ? null : { last, min, max }
}

/** Alternativa textual de una serie para `aria-label` ("CPU: ultimo 23,4%, minimo ..., maximo ..."). */
export function describeSeries(
  name: string,
  summary: SeriesSummary | null,
  format: (value: number) => string,
): string {
  if (!summary) return `${name}: sin datos`
  return `${name}: último ${format(summary.last)}, mínimo ${format(summary.min)}, máximo ${format(summary.max)}`
}
