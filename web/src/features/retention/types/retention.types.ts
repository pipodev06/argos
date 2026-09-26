export type RetentionSource = 'db' | 'env' | 'default'

/** GET/PUT /settings/retention. */
export interface RetentionSettings {
  detail_days: number
  source: RetentionSource
  /** Solo lectura: dias de detalle "caliente" sin comprimir. */
  hot_days: number
  /** Solo lectura: dias que se conserva el resumen por hora. */
  hourly_days: number
}

/** Origen de la retencion efectiva de un host o grupo. */
export type EffectiveSource = 'host' | 'group' | 'global'

/** Lo que borraria reducir la retencion (`error.details` del 409 `shrink_needs_confirm`). */
export interface ShrinkDetails {
  days_lost: number
  chunks: number
  est_bytes: number
}
