import type { HostStatus } from '@/features/hosts/types/host.types'

export type AlertSeverity = 'info' | 'warn' | 'critical'
export type AlertState = 'open' | 'acknowledged' | 'resolved'
export type RuleKind = 'metric' | 'host_status' | 'no_data' | 'service_check' | 'exposed_port'
export type RuleMetric = 'cpu_pct' | 'mem_pct' | 'disk_max_pct' | 'battery_pct' | 'load1' | 'load5' | 'load15'
export type RuleOp = 'gt' | 'lt'
/** `device` solo lo usan los chequeos ping. */
export type ScopeType = 'all' | 'group' | 'device'
export type ResolveReason = 'recovered' | 'manual' | 'rule_disabled' | 'rule_deleted' | 'host_disabled'
export type HostEventSource = 'detector' | 'ingest' | 'admin' | 'group_change'
export type MaintenanceStatus = 'scheduled' | 'active' | 'ended'

export interface ExpectedPort {
  proto: 'tcp' | 'udp'
  port: number
}

export interface Alert {
  id: string
  rule_id: string | null
  rule_name: string
  kind: RuleKind
  metric: RuleMetric | null
  threshold: number | null
  severity: AlertSeverity
  state: AlertState
  suppressed: boolean
  opened_at: string
  trigger_value: number | null
  /** exposed_port: "tcp/22, udp/53" (máx 5); null sin permiso de inventario. */
  detail?: string | null
  peak_value: number | null
  host: { id: string; hostname: string }
  acknowledged_by: string | null
  acknowledged_at: string | null
  resolved_at: string | null
  resolved_by: string | null
  resolve_reason: ResolveReason | null
}

export interface AlertListResponse {
  items: Alert[]
  next_cursor: string | null
  total: number
}

export interface Delivery {
  id: number
  channel_id: string
  channel_name: string
  event: 'opened' | 'resolved' | 'reminder'
  seq: number
  status: 'pending' | 'sending' | 'sent' | 'dead' | 'suppressed'
  attempts: number
  created_at: string
  sent_at: string | null
}

export interface HostEvent {
  id: number
  from_status: HostStatus
  to_status: HostStatus
  at: string
  source: HostEventSource
}

export interface AlertDetailResponse {
  alert: Alert
  deliveries: Delivery[]
  host_events: HostEvent[]
}

export interface AlertSummary {
  active: number
  suppressed: number
  by_severity: Partial<Record<AlertSeverity, { open: number; acknowledged: number }>>
}

export interface Scope {
  type: ScopeType
  group_id: string | null
  group_name: string | null
  /** Solo chequeos ping con alcance de un dispositivo. */
  device_id?: string
  device_name?: string | null
}

/** Alcance que se envía al servidor (solo los campos del tipo elegido). */
export interface ScopeInput {
  type: ScopeType
  group_id?: string
  device_id?: string
}

export interface AlertRule {
  id: string
  name: string
  kind: RuleKind
  metric: RuleMetric | null
  op: RuleOp | null
  threshold: number | null
  clear_threshold: number | null
  duration_seconds: number
  resolve_seconds: number
  include_offline: boolean
  check_id: string | null
  check_name: string | null
  warn_or_down: boolean
  scope: Scope
  severity: AlertSeverity
  enabled: boolean
  active_alerts: number
  channel_ids: string[]
  /** Solo exposed_port; otros tipos devuelven []. */
  expected_ports?: ExpectedPort[]
}

export interface RuleInput {
  name: string
  op?: RuleOp
  threshold?: number
  /** null borra el umbral de recuperación (solo PATCH). */
  clear_threshold?: number | null
  duration_seconds: number
  resolve_seconds: number
  include_offline?: boolean
  /** Solo service_check (alertar también en advertencia). */
  warn_or_down?: boolean
  /** Una regla service_check hereda el alcance del chequeo: no se envía. */
  scope?: ScopeInput
  /** Solo exposed_port; en PATCH reemplaza la lista. */
  expected_ports?: ExpectedPort[]
  severity: AlertSeverity
  enabled: boolean
}

export interface CreateRuleInput extends RuleInput {
  kind: RuleKind
  metric?: RuleMetric
  /** Solo service_check; no se puede cambiar después. */
  check_id?: string
}

export interface MaintenanceWindow {
  id: string
  name: string
  starts_at: string
  ends_at: string
  scope: Scope
  status: MaintenanceStatus
}

export interface MaintenanceInput {
  name: string
  starts_at?: string
  ends_at?: string
  scope: ScopeInput
}
