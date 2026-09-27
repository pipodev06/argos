import type { CheckName, ServiceLevel } from '../types/systemStatus.types'

export const CHECK_LABELS: Record<CheckName, string> = {
  maintenance: 'Mantenimiento',
  evaluator: 'Evaluador de alertas',
  notification_queue: 'Cola de notificaciones',
  notification_dead: 'Entregas fallidas',
}

export const LEVEL_LABELS: Record<ServiceLevel, string> = { ok: 'Normal', warn: 'Advertencia', crit: 'Crítico' }

export function checkLabel(name: string): string {
  return name in CHECK_LABELS ? CHECK_LABELS[name as CheckName] : name
}

/** Duración aproximada con las dos unidades mayores: "3 d 4 h", "2 h 5 min", "45 s". */
export function formatDuration(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds))
  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  const m = Math.floor((s % 3600) / 60)
  if (d > 0) return h > 0 ? `${d} d ${h} h` : `${d} d`
  if (h > 0) return m > 0 ? `${h} h ${m} min` : `${h} h`
  if (m > 0) return `${m} min`
  return `${s} s`
}

/** "hace 5 min" o "Sin ciclos aún" si nunca corrió. */
export function ageText(ageSeconds: number | null): string {
  return ageSeconds === null ? 'Sin ciclos aún' : `hace ${formatDuration(ageSeconds)}`
}
