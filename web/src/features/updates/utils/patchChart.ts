import type { UpdatesPoint } from '../types/updates.types'
import { countText } from './updates'

/** Fila de la gráfica: `t` en ms epoch; null = sin dato (hueco). */
export interface PatchRow {
  t: number
  total: number | null
  security: number | null
  reboot: boolean
}

/** Puntos del servidor (del más nuevo al más viejo) -> filas ordenadas por tiempo ascendente; descarta fechas inválidas. */
export function buildPatchRows(history: readonly UpdatesPoint[]): PatchRow[] {
  return history
    .map((p) => ({ t: Date.parse(p.at), total: p.pending_total, security: p.pending_security, reboot: p.reboot_required === true }))
    .filter((r) => !Number.isNaN(r.t))
    .sort((a, b) => a.t - b.t)
}

/** Alternativa textual de la gráfica (role="img"); el detalle completo está en la tabla. */
export function describePatchRows(rows: readonly PatchRow[]): string {
  const last = rows.at(-1)
  if (!last) return 'Sin datos de parches en el historial.'
  const reboots = rows.filter((r) => r.reboot).length
  return (
    `Pendientes y de seguridad en ${rows.length} puntos. Último: ${countText(last.total)} pendientes, ` +
    `${countText(last.security)} de seguridad. Reinicio pendiente en ${reboots} puntos.`
  )
}
