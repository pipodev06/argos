// Umbrales globales de uso (disco y similares), compartidos por la UI y, cuando exista, cualquier otro consumidor.
export const USAGE_WARN_PCT = 80
export const USAGE_CRIT_PCT = 90

export type UsageLevel = 'ok' | 'warn' | 'critical'

/** Lleva el valor al rango 0-100 (no finito = 0). */
export function clampPct(pct: number): number {
  if (!Number.isFinite(pct)) return 0
  return Math.min(100, Math.max(0, pct))
}

/** Porcentaje entero mostrado: se trunca (no se redondea) para que "80 %" en pantalla siempre sea >= al umbral. */
export function displayPct(pct: number): number {
  return Math.floor(clampPct(pct))
}

export function usageLevel(pct: number): UsageLevel {
  const value = clampPct(pct)
  if (value >= USAGE_CRIT_PCT) return 'critical'
  if (value >= USAGE_WARN_PCT) return 'warn'
  return 'ok'
}
