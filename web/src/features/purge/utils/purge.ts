import { ApiError } from '@/lib/api'
import type { GroupPurgeResult } from '../types/purge.types'

/** La confirmacion exige el nombre exacto (el servidor tambien lo compara). */
export function purgeNameMatches(typed: string, expected: string): boolean {
  return expected !== '' && typed === expected
}

function mismatch(err: ApiError): string | null {
  return err.code === 'confirm_mismatch' ? 'El nombre escrito no coincide.' : null
}

/** Errores de POST /hosts/{id}/purge; null = usar el mensaje del servidor. */
export function mapPurgeHostError(err: ApiError): string | null {
  if (err.code === 'collector_has_devices') {
    const n = (err.details as { devices?: unknown } | null | undefined)?.devices
    const who = typeof n === 'number' && n > 0 ? `${n.toLocaleString('es-PE')} ${n === 1 ? 'dispositivo' : 'dispositivos'}` : 'dispositivos'
    return `Este host aún es colector de ${who}. Antes, cámbiales el colector o elimínalos en Dispositivos.`
  }
  return mismatch(err)
}

/** Errores de POST /groups/{id}/purge; null = usar el mensaje del servidor. */
export function mapPurgeGroupError(err: ApiError): string | null {
  if (err.code === 'purge_in_progress') return 'Ya hay otra baja de cliente en curso: espera a que termine y vuelve a intentarlo.'
  return mismatch(err)
}

/** Lineas de resumen del resultado de la baja de un grupo (solo cuentas). */
export function summarizeGroupPurge(r: GroupPurgeResult): string[] {
  const n = (v: number) => v.toLocaleString('es-PE')
  return [
    `Hosts con agente eliminados: ${n(r.agents)}`,
    `Dispositivos eliminados: ${n(r.devices)}`,
    `Filas de métricas borradas: ${n(r.deleted_rows.metrics)} (más ${n(r.deleted_rows.metrics_hourly)} de resumen horario y ${n(r.deleted_rows.metrics_chunks)} bloques de detalle)`,
    `Reglas: ${n(r.rules)}, ventanas de mantenimiento: ${n(r.windows)}, chequeos: ${n(r.checks)}, tokens de enrolamiento: ${n(r.enroll_tokens)}`,
    `Usuarios deshabilitados: ${n(r.users_disabled)}; con alcance reducido: ${n(r.users_scope_reduced)}`,
    r.group_deleted ? 'El grupo se eliminó.' : 'El grupo se conservó (vacío).',
  ]
}
