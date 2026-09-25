export function formatAvailabilityPercent(pct: number | null): string {
  if (pct === null) return 'Sin datos'
  // pct viene como decimal (0-1), convertir a porcentaje con 1 decimal
  const percentage = pct * 100
  const rounded = Math.round(percentage * 10) / 10
  return `${rounded.toFixed(1)}%`
}

export function formatSeconds(seconds: number): string {
  if (seconds === 0) return '0 s'
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const secs = seconds % 60

  const parts: string[] = []
  if (days > 0) parts.push(`${days} d`)
  if (hours > 0) parts.push(`${hours} h`)
  if (minutes > 0) parts.push(`${minutes} m`)
  if (secs > 0 || parts.length === 0) parts.push(`${secs} s`)

  return parts.slice(0, 2).join(' ')
}

export function formatCoveragePct(pct: number): string {
  // pct viene como decimal (0-1), convertir a porcentaje entero (sin decimales)
  const percentage = pct * 100
  return `${Math.round(percentage)}%`
}

export const RANGE_LABELS: Record<'7d' | '30d' | '90d', string> = {
  '7d': '7 días',
  '30d': '30 días',
  '90d': '90 días',
}

export const VIEW_LABELS: Record<'host' | 'group', string> = {
  host: 'Por host',
  group: 'Por grupo',
}
