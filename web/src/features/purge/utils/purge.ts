import { formatFullDate } from '@/features/hosts/utils/format'
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

/** Nombre del archivo de la constancia: `constancia-baja-<grupo saneado>-<AAAAMMDD>.txt` (fecha local). */
export function constanciaFilename(groupName: string, purgedAt: string): string {
  const d = new Date(purgedAt)
  const ymd = `${d.getFullYear()}${String(d.getMonth() + 1).padStart(2, '0')}${String(d.getDate()).padStart(2, '0')}`
  const slug =
    groupName
      .normalize('NFD')
      .replace(/[̀-ͯ]/g, '')
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-+|-+$/g, '') || 'cliente'
  return `constancia-baja-${slug}-${ymd}.txt`
}

/** Texto plano de la constancia de baja: solo cuentas, nunca nombres de hosts. */
export function buildConstancia(r: GroupPurgeResult): string {
  const n = (v: number) => v.toLocaleString('es-PE')
  const failed = r.failed_host_ids.length
  const lines = [
    'CONSTANCIA DE BAJA DE CLIENTE',
    '',
    `Cliente: ${r.group_name}`,
    `Fecha y hora: ${formatFullDate(r.purged_at)}`,
    `Ejecutado por: ${r.performed_by}`,
    '',
    `Hosts con agente eliminados: ${n(r.agents)}`,
    `Dispositivos eliminados: ${n(r.devices)}`,
    `Filas de métricas borradas: ${n(r.deleted_rows.metrics)}`,
    `Filas de resumen horario borradas: ${n(r.deleted_rows.metrics_hourly)}`,
    `Bloques de detalle borrados: ${n(r.deleted_rows.metrics_chunks)}`,
    `Reglas: ${n(r.rules)}`,
    `Ventanas de mantenimiento: ${n(r.windows)}`,
    `Chequeos: ${n(r.checks)}`,
    `Tokens de enrolamiento: ${n(r.enroll_tokens)}`,
    `Canales de notificación: ${n(r.channels)}`,
    `Perfiles SNMP: ${n(r.snmp_profiles)}`,
    `Usuarios deshabilitados: ${n(r.users_disabled)}`,
    `Usuarios con alcance reducido: ${n(r.users_scope_reduced)}`,
    `Grupo eliminado: ${r.group_deleted ? 'sí' : 'no (se conservó vacío)'}`,
  ]
  if (failed > 0) lines.push(`Hosts que fallaron y no se purgaron: ${n(failed)}`)
  lines.push('', 'Los respaldos se rotan a los 14 días; la auditoría no se borra.')
  return lines.join('\n') + '\n'
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
