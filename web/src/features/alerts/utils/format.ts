import { formatNumber } from '@/features/hosts/utils/format'
import type {
  Alert,
  AlertRule,
  AlertSeverity,
  AlertState,
  HostEventSource,
  MaintenanceStatus,
  ResolveReason,
  RuleKind,
  RuleMetric,
  Scope,
} from '../types/alert.types'

export const SEVERITY_LABELS: Record<AlertSeverity, string> = { info: 'Info', warn: 'Advertencia', critical: 'Crítica' }
export const STATE_LABELS: Record<AlertState, string> = { open: 'Abierta', acknowledged: 'Reconocida', resolved: 'Resuelta' }
export const KIND_LABELS: Record<RuleKind, string> = {
  metric: 'Métrica',
  host_status: 'Estado del host',
  no_data: 'Sin datos',
  service_check: 'Chequeo',
}
export const METRIC_LABELS: Record<RuleMetric, string> = {
  cpu_pct: 'CPU',
  mem_pct: 'Memoria',
  disk_max_pct: 'Disco (mayor uso)',
  battery_pct: 'Batería',
  load1: 'Carga (1 min)',
  load5: 'Carga (5 min)',
  load15: 'Carga (15 min)',
}
export const RESOLVE_REASON_LABELS: Record<ResolveReason, string> = {
  recovered: 'La condición se recuperó',
  manual: 'Resuelta manualmente',
  rule_disabled: 'La regla se desactivó',
  rule_deleted: 'La regla se eliminó',
  host_disabled: 'El host se deshabilitó',
}
export const HOST_EVENT_SOURCE_LABELS: Record<HostEventSource, string> = {
  detector: 'Detector de inactividad',
  ingest: 'Reporte del agente',
  admin: 'Acción de un administrador',
  group_change: 'Cambio de grupo',
}
export const MAINTENANCE_STATUS_LABELS: Record<MaintenanceStatus, string> = {
  scheduled: 'Programada',
  active: 'En curso',
  ended: 'Finalizada',
}

export const isLoadMetric = (m: RuleMetric | null): boolean => m !== null && m.startsWith('load')

/** Estado visible de una alerta: si está en mantenimiento se muestra eso, no su estado. */
export function alertStateLabel(a: Pick<Alert, 'state' | 'suppressed'>): string {
  return a.suppressed && a.state !== 'resolved' ? 'En mantenimiento' : STATE_LABELS[a.state]
}

/** Segundos en la unidad exacta más grande: "90 s", "10 min", "2 h", "3 d". */
export function formatSeconds(seconds: number): string {
  if (seconds >= 86400 && seconds % 86400 === 0) return `${seconds / 86400} d`
  if (seconds >= 3600 && seconds % 3600 === 0) return `${seconds / 3600} h`
  if (seconds >= 60 && seconds % 60 === 0) return `${seconds / 60} min`
  return `${formatNumber(seconds, 0)} s`
}

/** Valor con su unidad: porcentaje, segundos sin reportar o carga (sin unidad). */
export function formatMetricValue(kind: RuleKind, metric: RuleMetric | null, value: number | null): string {
  if (value === null) return '—'
  if (kind === 'no_data') return formatSeconds(Math.round(value))
  if (kind === 'metric' && !isLoadMetric(metric)) return `${formatNumber(value, 2)} %`
  return formatNumber(value, 2)
}

/** "82,5 % (pico 95 %)"; el pico solo se muestra si difiere del valor de apertura. */
export function formatAlertValue(a: Pick<Alert, 'kind' | 'metric' | 'trigger_value' | 'peak_value'>): string {
  const base = formatMetricValue(a.kind, a.metric, a.trigger_value)
  if (a.trigger_value === null || a.peak_value === null || a.peak_value === a.trigger_value) return base
  return `${base} (pico ${formatMetricValue(a.kind, a.metric, a.peak_value)})`
}

/** Condición legible de una regla: "CPU > 90 % durante 10 min". */
export function describeCondition(
  r: Pick<AlertRule, 'kind' | 'metric' | 'op' | 'threshold' | 'duration_seconds'> & Partial<Pick<AlertRule, 'check_name' | 'warn_or_down'>>,
): string {
  const during = r.duration_seconds > 0 ? ` durante ${formatSeconds(r.duration_seconds)}` : ''
  if (r.kind === 'service_check') return `Chequeo ${r.check_name ?? 'eliminado'} ${r.warn_or_down ? 'en advertencia o caído' : 'caído'}${during}`
  if (r.kind === 'host_status') return `Host caído${during}`
  if (r.kind === 'no_data') return `Sin reportar por más de ${formatSeconds(Math.round(r.threshold ?? 0))}${during}`
  const label = r.metric ? METRIC_LABELS[r.metric] : 'Métrica'
  return `${label} ${r.op === 'lt' ? '<' : '>'} ${formatMetricValue(r.kind, r.metric, r.threshold)}${during}`
}

export function scopeLabel(s: Scope): string {
  if (s.type === 'group') return `Grupo: ${s.group_name ?? 'eliminado'}`
  if (s.type === 'tag') return `Etiqueta: ${s.tag ?? ''}`
  return 'Todos los hosts'
}

/** Número desde un campo de texto (admite coma decimal); null si está vacío o no es válido. */
export function parseNumber(raw: string): number | null {
  const t = raw.trim().replace(',', '.')
  if (t === '') return null
  const n = Number(t)
  return Number.isFinite(n) ? n : null
}
