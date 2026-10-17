import type { Scope, ScopeInput } from '@/features/alerts/types/alert.types'

export type CheckType = 'tcp' | 'http' | 'tls_cert' | 'service' | 'ping' | 'snmp'
export type CheckStatus = 'up' | 'down' | 'warn' | 'unknown'

export interface ServiceCheck {
  id: string
  name: string
  type: CheckType
  interval_seconds: number
  timeout_ms: number
  retries: number
  target_host: string | null
  target_port: number | null
  url: string | null
  expect_status: string | null
  expect_text: string | null
  service_name: string | null
  warn_ms: number | null
  crit_ms: number | null
  warn_days: number | null
  crit_days: number | null
  /** Solo snmp: `reachable` | `if_oper`. */
  metric?: string | null
  /** Solo snmp con `if_oper`. */
  if_index?: number | null
  /** Solo lo ve un admin. */
  header_name?: string | null
  has_secret: boolean
  scope: Scope
  enabled: boolean
}

/** Cuerpo de crear/editar. Los null (donde el servidor los admite) quitan el valor; `secret` es solo de escritura. */
export interface CheckInput {
  name?: string
  type?: CheckType
  interval_seconds?: number
  timeout_ms?: number
  retries?: number
  target_host?: string
  target_port?: number
  url?: string
  expect_status?: string | null
  expect_text?: string | null
  service_name?: string
  warn_ms?: number | null
  crit_ms?: number | null
  warn_days?: number
  crit_days?: number
  header_name?: string
  metric?: string
  if_index?: number | null
  secret?: string | null
  scope?: ScopeInput
  enabled?: boolean
}

export interface CheckState {
  status: CheckStatus
  status_changed_at: string
  ts: string
  response_ms: number | null
  value: number | null
  detail: string | null
}

export interface HostCheck {
  /** `enabled: false` = chequeo pausado (se lista igual, con su último estado conocido). */
  check: { id: string; name: string; type: CheckType; target: string; interval_s: number; enabled: boolean }
  state: CheckState | null
}

export interface CheckEvent {
  id: number
  from_status: CheckStatus | null
  to_status: CheckStatus
  detail: string | null
  at: string
}

export interface CheckEventsPage {
  items: CheckEvent[]
  next_cursor: string | null
}
