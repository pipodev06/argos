import type { AlertSeverity } from '@/features/alerts/types/alert.types'

export interface Channel {
  id: string
  name: string
  kind: 'webhook'
  min_severity: AlertSeverity
  enabled: boolean
  has_secret: boolean
  /** Dueño: null = global (usable en cualquier regla). */
  group_id: string | null
  group_name: string | null
  /** Solo lo devuelve la API a administradores; nunca incluye la URL completa. */
  config?: { host: string; signed: boolean }
}

export interface CreateChannelInput {
  name: string
  kind: 'webhook'
  url: string
  hmac_secret?: string
  min_severity: AlertSeverity
  enabled: boolean
  group_id: string | null
}

export interface UpdateChannelInput {
  name?: string
  url?: string
  /** null quita la firma. */
  hmac_secret?: string | null
  min_severity?: AlertSeverity
  enabled?: boolean
  group_id?: string | null
}

export type TestCategory = 'dns' | 'connect' | 'tls' | 'timeout' | 'status'

export interface TestResult {
  ok: boolean
  category: TestCategory | null
}
