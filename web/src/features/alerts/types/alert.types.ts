import type { HostStatus } from '@/features/hosts/types/host.types'

export type AlertSeverity = 'info' | 'warn' | 'critical'
export type AlertState = 'open' | 'acknowledged' | 'resolved'
export type RuleKind = 'metric' | 'host_status' | 'no_data'
export type RuleMetric = 'cpu_pct' | 'mem_pct' | 'disk_max_pct' | 'battery_pct' | 'load1' | 'load5' | 'load15'
export type RuleOp = 'gt' | 'lt'
export type ScopeType = 'all' | 'group' | 'tag'
export type ResolveReason = 'recovered' | 'manual' | 'rule_disabled' | 'rule_deleted' | 'host_disabled'
export type HostEventSource = 'detector' | 'ingest' | 'admin' | 'group_change'
export type MaintenanceStatus = 'scheduled' | 'active' | 'ended'

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
  tag: string | null
}

/** Alcance que se envía al servidor (solo los campos del tipo elegido). */
export interface ScopeInput {
  type: ScopeType
  group_id?: string
  tag?: string
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
  scope: Scope
  severity: AlertSeverity
  enabled: boolean
  active_alerts: number
  channel_ids: string[]
}

export interface RuleInput {
  name: string
  op?: RuleOp
  threshold?: number
  /** null borra el umbral de recuperación (solo PATCH). */
  clear_threshold?: number | null
  duration_seconds: number
  resolve_seconds: number
  include_offline: boolean
  scope: ScopeInput
  severity: AlertSeverity
  enabled: boolean
}

export interface CreateRuleInput extends RuleInput {
  kind: RuleKind
  metric?: RuleMetric
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
