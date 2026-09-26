/** Umbral en segundos -> "5 min", "2 h" (ya legible; usa la unidad exacta mas grande). */
export function formatThreshold(seconds: number): string {
  if (seconds % 3600 === 0) return `${seconds / 3600} h`
  if (seconds % 60 === 0) return `${seconds / 60} min`
  return `${seconds} s`
}

export type ThresholdUnit = 'min' | 'h'

const MIN_DOWN_AFTER_S = 30
const MAX_DOWN_AFTER_S = 30 * 24 * 3600

/** Divide un umbral en cantidad + unidad para el formulario. */
export function splitThreshold(seconds: number): { amount: string; unit: ThresholdUnit } {
  if (seconds % 3600 === 0) return { amount: String(seconds / 3600), unit: 'h' }
  return { amount: String(Math.round((seconds / 60) * 100) / 100), unit: 'min' }
}

/** Convierte cantidad + unidad a segundos, o un mensaje de error si esta fuera del rango del servidor. */
export function parseThreshold(
  amount: string,
  unit: ThresholdUnit,
): { seconds: number } | { error: string } {
  const value = Number(amount.replace(',', '.'))
  if (!amount.trim() || !Number.isFinite(value)) return { error: 'Ingresa un número.' }
  const seconds = Math.round(value * (unit === 'h' ? 3600 : 60))
  if (seconds < MIN_DOWN_AFTER_S) return { error: 'El mínimo es 30 segundos (0,5 min).' }
  if (seconds > MAX_DOWN_AFTER_S) return { error: 'El máximo es 30 días.' }
  return { seconds }
}
