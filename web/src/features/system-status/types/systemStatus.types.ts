export type ServiceLevel = 'ok' | 'warn' | 'crit'
export type CheckName = 'maintenance' | 'evaluator' | 'notification_queue' | 'notification_dead'

export interface DeadChannel {
  channel_id: string
  name: string
  owner_group: { id: string; name: string } | null
  count: number
}

export interface ServiceCheck {
  name: CheckName
  status: ServiceLevel
  detail: string
}

export interface SystemStatus {
  status: ServiceLevel
  version: string
  started_at: string
  uptime_seconds: number
  notifications: {
    pending: number
    oldest_pending_age_seconds: number | null
    dead_24h_total: number
    dead_24h: DeadChannel[]
  }
  database: { size_bytes: number }
  checks: ServiceCheck[]
}
