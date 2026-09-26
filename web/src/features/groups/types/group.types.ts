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
}
