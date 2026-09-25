/** Contrato de GET /hosts/{id}/updates y GET /updates (todo campo `null` = desconocido, nunca 0). */
export interface Eol {
  label: string
  /** YYYY-MM-DD */
  eol_date: string
  days_left: number
  expired: boolean
  estimated: boolean
  lts: boolean
}

export interface HostUpdates {
  pending_total: number | null
  pending_security: number | null
  reboot_required: boolean | null
  reboot_since: string | null
  security_pending_since: string | null
  /** YYYY-MM-DD */
  last_installed: string | null
  source: string
  cache_age_s: number | null
  /** Cache de paquetes de mas de 7 dias: posiblemente desactualizado. */
  stale: boolean
  data_age_s: number
  collected_at: string
  updated_at: string
  advisories: string[]
  advisories_truncated: boolean
}

export interface UpdatesPoint {
  at: string
  pending_total: number | null
  pending_security: number | null
  reboot_required: boolean | null
}

/** `apiRequest` ya desenvuelve `{"data": ...}`. */
export interface HostUpdatesResponse {
  status: 'ok' | 'no_data'
  updates: HostUpdates | null
  eol: Eol | null
  history: UpdatesPoint[]
}

export interface FleetItem {
  host: { id: string; hostname: string }
  status: string
  group: { id: string; name: string } | null
  os: string
  os_name: string | null
  os_version: string | null
  pending_total: number | null
  pending_security: number | null
  reboot_required: boolean | null
  reboot_since: string | null
  last_installed: string | null
  source: string
  cache_age_s: number | null
  stale: boolean
  data_age_s: number
  updated_at: string
  eol: Eol | null
}

export interface FleetPage {
  items: FleetItem[]
  next_cursor: string | null
  total: number
}
