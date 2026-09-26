import type { MetricSample } from '../types/host.types'
import type { ChartView } from './chart'
import { formatClock, formatDate, formatDateHour, formatDateTime } from './format'

const MINUTE = 60_000
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR

/** Retencion por defecto del detalle de 30 s (ARGOS_METRICS_RETENTION_DAYS); el servidor decide la resolucion real. */
export const DETAIL_RETENTION_DAYS = 30
const RAW_MAX_DAYS = 31
const HOURLY_MAX_DAYS = 366
/** Margen que el servidor tolera hacia el futuro. */
const FUTURE_SLACK_MS = 5 * MINUTE

export type HistoryResolution = 'raw' | 'hourly'
export type HistorySeries = 'metrics' | 'disks'

export interface HistoryResponse {
  host: { id: string; hostname: string }
  resolution: HistoryResolution
  /** Retención efectiva del detalle de este host (host ?? grupo ?? global); ausente en servers antiguos. */
  detail_retention_days?: number
  from: string
  to: string
  step_seconds: number
  available_from: string | null
  points: MetricSample[]
}

export interface DateRange {
  fromMs: number
  toMs: number
}

const pad = (n: number) => String(n).padStart(2, '0')

/** Valor para `<input type="datetime-local">` (hora local, sin zona): "2026-09-25T14:05". */
export function toLocalInput(ms: number): string {
  const d = new Date(ms)
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** Valor para `<input type="date">` (fecha local). */
export function toLocalDate(ms: number): string {
  return toLocalInput(ms).slice(0, 10)
}

/** Lo escrito en un datetime-local se interpreta en hora local del navegador; NaN si esta vacio o es invalido. */
export function parseLocalInput(value: string): number {
  return value ? new Date(value).getTime() : NaN
}

/** Instante en UTC para la API ("2026-09-25T19:05:00.000Z"). */
export function toUtcIso(ms: number): string {
  return new Date(ms).toISOString()
}

/** Un dia local completo: 00:00 a 24:00 (usa el calendario, asi que respeta cambios de hora). */
export function dayRange(dateValue: string): DateRange | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(dateValue)
  if (!m) return null
  const [y, mo, d] = [Number(m[1]), Number(m[2]) - 1, Number(m[3])]
  return { fromMs: new Date(y, mo, d).getTime(), toMs: new Date(y, mo, d + 1).getTime() }
}

export type ShortcutId = 'yesterday' | 'last7' | 'thisMonth' | 'lastMonth'

export const SHORTCUTS: readonly { id: ShortcutId; label: string }[] = [
  { id: 'yesterday', label: 'Ayer' },
  { id: 'last7', label: 'Hace 7 días' },
  { id: 'thisMonth', label: 'Este mes' },
  { id: 'lastMonth', label: 'Mes pasado' },
]

export function shortcutRange(id: ShortcutId, now: number): DateRange {
  const d = new Date(now)
  const y = d.getFullYear()
  const mo = d.getMonth()
  const day = d.getDate()
  switch (id) {
    case 'yesterday':
      return { fromMs: new Date(y, mo, day - 1).getTime(), toMs: new Date(y, mo, day).getTime() }
    case 'last7':
      return { fromMs: now - 7 * DAY, toMs: now }
    case 'thisMonth':
      return { fromMs: new Date(y, mo, 1).getTime(), toMs: now }
    case 'lastMonth':
      return { fromMs: new Date(y, mo - 1, 1).getTime(), toMs: new Date(y, mo, 1).getTime() }
  }
}

/** Misma regla que el servidor: si `from` cae dentro de la retencion del detalle, se sirve crudo; si no, por hora. */
export function expectedResolution(fromMs: number, now: number, detailDays: number = DETAIL_RETENTION_DAYS): HistoryResolution {
  return fromMs >= now - detailDays * DAY ? 'raw' : 'hourly'
}

/** Mensaje de error del formulario, o null si el rango es valido. */
export function validateRange({ fromMs, toMs }: DateRange, now: number, detailDays: number = DETAIL_RETENTION_DAYS): string | null {
  if (Number.isNaN(fromMs) || Number.isNaN(toMs)) return 'Indica la fecha de inicio y la de fin.'
  if (toMs <= fromMs) return 'El fin debe ser posterior al inicio.'
  if (toMs > now + FUTURE_SLACK_MS) return 'El fin no puede estar en el futuro.'
  const raw = expectedResolution(fromMs, now, detailDays) === 'raw'
  const maxDays = raw ? RAW_MAX_DAYS : HOURLY_MAX_DAYS
  if (toMs - fromMs > maxDays * DAY) {
    return raw
      ? `Con detalle de 30 s el rango máximo es de ${RAW_MAX_DAYS} días.`
      : `Con resumen por hora el rango máximo es de ${HOURLY_MAX_DAYS} días.`
  }
  return null
}

/** Nombre de archivo del CSV cuando el servidor no manda `Content-Disposition`. */
export function csvFilename(series: HistorySeries, hostname: string, { fromMs, toMs }: DateRange): string {
  const prefix = series === 'disks' ? 'discos' : 'metricas'
  const safe = hostname.replace(/[^A-Za-z0-9._-]/g, '_')
  return `${prefix}-${safe}-${toLocalDate(fromMs)}_${toLocalDate(toMs)}.csv`
}

/** Nombre de archivo de `Content-Disposition` (rutas neutralizadas); null si no viene. */
export function filenameFromDisposition(header: string | null): string | null {
  const m = header ? /filename="?([^";]+)"?/i.exec(header) : null
  const name = m ? m[1].replace(/[\\/]/g, '_') : ''
  return name || null
}

// Escalones de marcas del eje X: el primero que deje <= 10 marcas.
const TICK_STEPS = [
  10 * MINUTE, 30 * MINUTE, HOUR, 2 * HOUR, 4 * HOUR, 6 * HOUR, 12 * HOUR,
  DAY, 2 * DAY, 5 * DAY, 15 * DAY, 30 * DAY,
]
const MAX_TICKS = 10

export function tickStepFor(spanMs: number): number {
  return TICK_STEPS.find((s) => spanMs / s <= MAX_TICKS) ?? TICK_STEPS[TICK_STEPS.length - 1]
}

/** Ejes de la grafica de un rango fijo: hora en rangos de hasta 2 dias, fecha (con ano si es largo) en los demas. */
export function historyView(fromMs: number, toMs: number, stepSeconds: number): ChartView {
  const span = toMs - fromMs
  return {
    startMs: fromMs,
    endMs: toMs,
    tickMs: tickStepFor(span),
    phrase: `entre ${formatDateTime(fromMs)} y ${formatDateTime(toMs)}`,
    formatTick: (ms) => (span <= 2 * DAY ? formatClock(ms) : formatDate(ms, span > 180 * DAY)),
    formatTooltip: (ms) => (stepSeconds >= 3600 ? formatDateHour(ms) : formatDateTime(ms)),
  }
}
