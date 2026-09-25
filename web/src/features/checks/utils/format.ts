import { formatNumber } from '@/features/hosts/utils/format'
import { scopeLabel } from '@/features/alerts/utils/format'
import type { CheckState, CheckType, HostCheck, ServiceCheck } from '../types/check.types'

export const CHECK_TYPE_LABELS: Record<CheckType, string> = { tcp: 'TCP', http: 'HTTP', tls_cert: 'Certificado TLS', service: 'Servicio', ping: 'Ping' }

const DETAIL_LABELS: Record<string, string> = {
  ok: 'Correcto',
  timeout: 'Tiempo de espera agotado',
  refused: 'Conexión rechazada',
  blocked: 'Destino no permitido por la política local del agente',
  tls: 'Error de TLS',
  not_found: 'No encontrado',
  denied: 'Acceso denegado',
  unsupported: 'No soportado en este sistema',
  status_mismatch: 'Estado distinto al esperado',
  text_mismatch: 'El texto esperado no aparece',
  expired: 'Certificado vencido',
}

export const detailLabel = (detail: string | null): string => (detail ? (DETAIL_LABELS[detail] ?? detail) : '—')

/** Alcance legible: en un ping, "todos" son los dispositivos (no los hosts con agente). */
export function checkScopeLabel(c: ServiceCheck): string {
  return c.type === 'ping' && c.scope.type === 'all' ? 'Todos los dispositivos' : scopeLabel(c.scope)
}

/** Destino legible de una definición (la URL ya viene sin query para el viewer; el admin la ve completa en el formulario). */
export function checkTarget(c: ServiceCheck): string {
  if (c.type === 'service') return c.service_name ?? '—'
  if (c.type === 'ping') return c.scope.type === 'device' ? (c.scope.device_name ?? '—') : 'Dirección del dispositivo'
  if (c.type === 'http') return c.url ? c.url.split('?')[0] : '—'
  return c.target_host && c.target_port ? `${c.target_host}:${c.target_port}` : '—'
}

/** Latencia (ms) o días restantes (tls_cert) del último resultado. */
export function checkValue(type: CheckType, s: CheckState | null): string {
  if (!s) return '—'
  if (type === 'tls_cert') return s.value === null ? '—' : `${formatNumber(s.value, 0)} d`
  if (type === 'service') return '—'
  return s.response_ms === null ? '—' : `${formatNumber(s.response_ms, 0)} ms`
}

export const hostCheckValue = (c: HostCheck): string => checkValue(c.check.type, c.state)
