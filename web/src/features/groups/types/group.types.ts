/** Cuotas del cliente (B7; "cliente" = grupo, 1 a 1); null = sin límite propio. */
export interface GroupQuotas {
  max_hosts: number | null
  max_devices: number | null
  max_checks: number | null
  max_users: number | null
  max_rules: number | null
  max_channels: number | null
}

export interface Group {
  id: string
  name: string
  down_after_seconds: number | null
  always_on: boolean
  host_count: number
  /** Dias de detalle propios del grupo; null = hereda el global. */
  detail_retention_days: number | null
  effective_detail_days: number
  retention_source: 'group' | 'global'
  quotas: GroupQuotas
}

export interface GroupUsageItem {
  current: number
  limit: number | null
}

/** Consumo actual de cada recurso con cuota (GET /groups/{id}/usage). */
export interface GroupUsage {
  hosts: GroupUsageItem
  devices: GroupUsageItem
  checks: GroupUsageItem
  users: GroupUsageItem
  rules: GroupUsageItem
  channels: GroupUsageItem
}

export interface GroupListResponse {
  items: Group[]
  ungrouped: { host_count: number }
  defaults: { down_after_seconds: number }
}

export interface GroupInput {
  name: string
  down_after_seconds: number | null
  always_on: boolean
  /** Solo al editar: null = heredar. Omitido = no cambia. */
  detail_retention_days?: number | null
  confirm_shrink?: boolean
  /** Cuotas (B7, solo al editar): omitido = no cambia; null = sin límite. */
  max_hosts?: number | null
  max_devices?: number | null
  max_checks?: number | null
  max_users?: number | null
  max_rules?: number | null
  max_channels?: number | null
}
