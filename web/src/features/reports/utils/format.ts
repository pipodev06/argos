/** Objetivo de disponibilidad: a partir de aqui el estado es "cumple". */
export const AVAILABILITY_TARGET_PCT = 99

/** `availability_pct` y `coverage_pct` de la API van en 0-100. */
export function formatAvailabilityPercent(pct: number | null): string {
  if (pct === null) return 'Sin datos'
  return `${(Math.round(pct * 10) / 10).toFixed(1)}%`
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
  return `${Math.round(pct)}%`
}



/** Fecha desde la que hay historial ("Desde"); sin dato = guion. */
export function formatTrackedFrom(iso: string | null): string {
  return iso ? new Date(iso).toLocaleDateString('es-PE') : '—'
}
