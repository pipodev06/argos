import type { AuditOutcome } from '../types/audit.types'

const EVENT_LABELS: Record<string, string> = {
  login_ok: 'Inicio de sesión',
  login_fail: 'Inicio de sesión fallido',
  logout: 'Cierre de sesión',
  password_changed: 'Cambio de contraseña',
  user_created: 'Usuario creado',
  user_role_changed: 'Rol de usuario cambiado',
  user_disabled: 'Usuario deshabilitado',
  user_enabled: 'Usuario habilitado',
  user_password_reset: 'Contraseña restablecida',
  enroll_token_created: 'Token de enrolamiento creado',
  host_enrolled: 'Host enrolado',
  host_revoked: 'Host revocado',
  group_created: 'Grupo creado',
  group_updated: 'Grupo actualizado',
  group_deleted: 'Grupo eliminado',
  host_group_set: 'Grupo de host asignado',
  hosts_bulk: 'Acción masiva de hosts',
  alert_acked: 'Alerta reconocida',
  alert_resolved: 'Alerta resuelta',
  alert_rule_created: 'Regla creada',
  alert_rule_updated: 'Regla actualizada',
  alert_rule_deleted: 'Regla eliminada',
  alert_rule_channels_set: 'Canales de regla asignados',
  channel_created: 'Canal creado',
  channel_updated: 'Canal actualizado',
  channel_deleted: 'Canal eliminado',
  channel_tested: 'Canal probado',
  maintenance_created: 'Mantenimiento creado',
  maintenance_updated: 'Mantenimiento actualizado',
  maintenance_deleted: 'Mantenimiento eliminado',
  authz_denied: 'Acceso denegado',
  user_permissions_changed: 'Permisos de usuario cambiados',
  host_renamed: 'Host renombrado',
  host_purged: 'Datos de host eliminados',
  group_purged: 'Baja de cliente (grupo purgado)',
  retention_updated: 'Retención de datos cambiada',
  history_export: 'Historial exportado',
  report_exported: 'Informe exportado',
  inventory_read: 'Inventario consultado',
  updates_read: 'Parches consultados',
  device_created: 'Dispositivo creado',
  device_updated: 'Dispositivo actualizado',
  device_deleted: 'Dispositivo eliminado',
  snmp_profile_created: 'Perfil SNMP creado',
  snmp_profile_updated: 'Perfil SNMP actualizado',
  snmp_profile_deleted: 'Perfil SNMP eliminado',
  service_check_created: 'Chequeo creado',
  service_check_updated: 'Chequeo actualizado',
  service_check_deleted: 'Chequeo eliminado',
  agent_auth_fail: 'Autenticación de agente fallida',
  enroll_fail: 'Enrolamiento fallido',
}

/** Nombre legible del evento; los desconocidos se muestran tal cual. */
export function eventLabel(event: string): string {
  return EVENT_LABELS[event] ?? event
}

export const OUTCOME_LABELS: Record<AuditOutcome, string> = {
  ok: 'Correcto',
  fail: 'Fallido',
  denied: 'Denegado',
}

export function formatAuditDate(iso: string): string {
  return new Date(iso).toLocaleString('es-PE', {
    day: '2-digit',
    month: '2-digit',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  })
}

/** Id largo abreviado ("a1b2c3d4…"); los cortos van completos. */
export function shortId(id: string): string {
  return id.length > 12 ? `${id.slice(0, 8)}…` : id
}

/** Valor de `detail` como texto plano (los objetos anidados, en JSON). */
export function formatDetailValue(value: unknown): string {
  if (value === null || value === undefined) return '—'
  if (typeof value === 'string') return value === '' ? '—' : value
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}
