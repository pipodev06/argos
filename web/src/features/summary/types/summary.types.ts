import type { GroupRef } from '@/features/hosts/types/host.types'

export interface SummaryGroup {
  group_id: string | null
  name: string
  total: number
  up: number
  down: number
  offline: number
  pending: number
  disabled: number
}

export interface TopHost {
  id: string
  hostname: string
  value: number
  /** Solo en `top_disk`. */
  mount?: string
}

export interface RecentDown {
  id: string
  hostname: string
  status_changed_at: string
  group: GroupRef | null
}

export interface Summary {
  generated_at: string
  counts: { up: number; down: number; offline: number; pending: number; disabled: number }
  by_group: SummaryGroup[]
  top_cpu: TopHost[]
  top_mem: TopHost[]
  top_disk: TopHost[]
  recently_down: RecentDown[]
}
