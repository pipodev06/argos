type ElapsedUnit = 's' | 'min' | 'h' | 'd'

function elapsedSince(iso: string, now: number): { value: number; unit: ElapsedUnit } {
  const seconds = Math.max(0, Math.floor((now - new Date(iso).getTime()) / 1000))
  if (seconds < 60) return { value: seconds, unit: 's' }
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return { value: minutes, unit: 'min' }
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return { value: hours, unit: 'h' }
  return { value: Math.floor(hours / 24), unit: 'd' }
}

/** Compacto para listas: "hace 5min" ("nunca" si no hay reporte). */
export function formatLastSeen(lastSeenAt: string | null, now: number = Date.now()): string {
  if (!lastSeenAt) return 'nunca'
  const { value, unit } = elapsedSince(lastSeenAt, now)
  return `hace ${value}${unit}`
}

/** Duracion para prosa: "5 min", "3 h". */
export function formatElapsed(iso: string, now: number = Date.now()): string {
  const { value, unit } = elapsedSince(iso, now)
  return `${value} ${unit}`
}

export function formatExpiry(expiresAt: string): string {
  return new Date(expiresAt).toLocaleTimeString('es-PE', { hour: '2-digit', minute: '2-digit' })
}

const LOCALE = 'es-PE'

/** Numero con hasta `maxDigits` decimales (sin ceros sobrantes) y separadores es-PE. */
export function formatNumber(value: number, maxDigits = 1): string {
  return value.toLocaleString(LOCALE, { maximumFractionDigits: maxDigits })
}

export function formatPercent(pct: number): string {
  return `${formatNumber(pct)}%`
}

/** Memoria en MB (base 1024): < 1024 -> "512 MB", si no "3,4 GB". */
export function formatMemory(mb: number): string {
  return mb < 1024 ? `${formatNumber(mb, 0)} MB` : `${formatNumber(mb / 1024)} GB`
}

/** Tasa en bytes/s (base 1000, convencion de redes): "850 B/s", "12,3 KB/s", "4,5 MB/s". */
export function formatBps(bytesPerSecond: number): string {
  if (bytesPerSecond < 1000) return `${formatNumber(bytesPerSecond, 0)} B/s`
  if (bytesPerSecond < 1_000_000) return `${formatNumber(bytesPerSecond / 1000)} KB/s`
  if (bytesPerSecond < 1_000_000_000) return `${formatNumber(bytesPerSecond / 1_000_000)} MB/s`
  return `${formatNumber(bytesPerSecond / 1_000_000_000)} GB/s`
}

/** Hora local 24 h ("14:05"). */
export function formatClock(ms: number): string {
  return new Date(ms).toLocaleTimeString(LOCALE, { hour: '2-digit', minute: '2-digit', hour12: false })
}

/** Dia local corto ("lun 23"), para el eje de 7 dias. */
export function formatDay(ms: number): string {
  return new Date(ms).toLocaleDateString(LOCALE, { weekday: 'short', day: 'numeric' })
}

/** Fecha local corta ("23 sept"; con `withYear`, "23 sept 2026"), para rangos largos. */
export function formatDate(ms: number, withYear = false): string {
  return new Date(ms).toLocaleDateString(LOCALE, {
    day: 'numeric',
    month: 'short',
    ...(withYear && { year: 'numeric' }),
  })
}

/** Fecha y hora local sin segundos ("23 sept, 14:00"), para el tooltip de 30 dias (pasos de 1 h). */
export function formatDateHour(ms: number): string {
  return new Date(ms).toLocaleString(LOCALE, {
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
}

/** Fecha y hora local completa, para el tooltip ("23 sept, 14:05:30"). */
export function formatDateTime(ms: number): string {
  return new Date(ms).toLocaleString(LOCALE, {
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  })
}

/** Capacidad en MB (base 1024) sin decimales sobrantes en TB: "512 MB", "3,4 GB", "1,8 TB". */
export function formatCapacity(mb: number): string {
  return mb >= 1024 * 1024 ? `${formatNumber(mb / (1024 * 1024))} TB` : formatMemory(mb)
}

/** Fecha y hora local sin segundos ("23 sept 2026, 14:05"), para fechas puntuales del detalle. */
export function formatFullDate(iso: string): string {
  return new Date(iso).toLocaleString(LOCALE, {
    day: 'numeric',
    month: 'short',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
}
