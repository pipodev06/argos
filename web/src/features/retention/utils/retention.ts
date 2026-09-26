import { ApiError } from '@/lib/api'
import type { EffectiveSource, RetentionSource, ShrinkDetails } from '../types/retention.types'

export const MIN_DETAIL_DAYS = 8
export const MAX_DETAIL_DAYS = 730

/**
 * Peso estimado del detalle comprimido de un host por dia: ~8 B por muestra x 2880 muestras (una cada 30 s) = ~23 KB.
 * Origen: docs/decisiones/2026-09-26-detalle-comprimido-retencion.md (metrics_chunks). Es una estimacion, no una medida.
 */
export const EST_BYTES_PER_HOST_DAY = 23 * 1024

const RANGE_MSG = `Ingresa un número entero de ${MIN_DETAIL_DAYS} a ${MAX_DETAIL_DAYS}.`

/** Valida los dias de retencion escritos en un input; el texto se limpia de espacios. */
export function parseDetailDays(raw: string): { days: number } | { error: string } {
  const t = raw.trim()
  if (!/^\d{1,4}$/.test(t)) return { error: RANGE_MSG }
  const days = Number(t)
  if (days < MIN_DETAIL_DAYS || days > MAX_DETAIL_DAYS) return { error: RANGE_MSG }
  return { days }
}

/** Peso estimado del detalle: por host y, si se conoce el total, de toda la flota. */
export function estimateWeight(days: number, hosts: number): { perHost: number; total: number } {
  const perHost = days * EST_BYTES_PER_HOST_DAY
  return { perHost, total: perHost * Math.max(0, hosts) }
}

const UNITS = ['B', 'KB', 'MB', 'GB', 'TB'] as const

/** Bytes legibles en base 1024 (1 decimal por debajo de 10, sin decimales sobre 10). */
export function formatBytes(bytes: number): string {
  let v = Math.max(0, bytes)
  let i = 0
  while (v >= 1024 && i < UNITS.length - 1) {
    v /= 1024
    i++
  }
  const digits = i === 0 || v >= 10 ? 0 : 1
  return `${v.toLocaleString('es-PE', { maximumFractionDigits: digits, minimumFractionDigits: digits })} ${UNITS[i]}`
}

/** Si el error es el 409 `shrink_needs_confirm`, devuelve lo que se perderia; si no, null. */
export function parseShrink(err: unknown): ShrinkDetails | null {
  if (!(err instanceof ApiError) || err.code !== 'shrink_needs_confirm') return null
  const d = err.details as Partial<ShrinkDetails> | undefined
  if (!d || typeof d.days_lost !== 'number' || typeof d.chunks !== 'number' || typeof d.est_bytes !== 'number') {
    return { days_lost: 0, chunks: 0, est_bytes: 0 }
  }
  return { days_lost: d.days_lost, chunks: d.chunks, est_bytes: d.est_bytes }
}

export function sourceLabel(source: RetentionSource | EffectiveSource | string): string {
  switch (source) {
    case 'db':
      return 'Configurada aquí'
    case 'env':
      return 'Variable de entorno del servidor'
    case 'default':
      return 'Valor por defecto'
    case 'host':
      return 'Propia del host'
    case 'group':
      return 'Heredada del grupo'
    case 'global':
      return 'Global'
    default:
      return source
  }
}
