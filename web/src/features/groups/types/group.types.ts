export interface Group {
  id: string
  name: string
  down_after_seconds: number | null
  always_on: boolean
  host_count: number
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
}
