export type AuditOutcome = 'ok' | 'fail' | 'denied'

export interface AuditEntry {
  id: number
  ts: string
  event: string
  outcome: AuditOutcome
  actor_user_id: string | null
  actor_username: string | null
  target_type: string | null
  target_id: string | null
  ip: string | null
  /** Objeto ya saneado por el servidor (puede venir vacio). */
  detail: Record<string, unknown>
}

export interface AuditListResponse {
  items: AuditEntry[]
  next_cursor: string | null
  total: number
}

export interface AuditEventsResponse {
  events: string[]
}
