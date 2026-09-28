export type ReportRange = '7d' | '30d' | '90d' | 'custom'
export type ReportView = 'host' | 'group'

export interface ReportFleet {
  hosts: number
  up_s: number
  down_s: number
  down_maint_s: number
  offline_s: number
  incidents: number
  availability_pct: number | null
  coverage_pct: number
}

export interface AvailabilityItem extends ReportFleet {
  host_id: string
  hostname: string
  group_id: string | null
  group_name: string | null
  tracked_from: string
}

export interface AvailabilityGroupItem extends ReportFleet {
  group_id: string | null
  group_name: string | null
  hosts: number
}

export interface AvailabilityReport {
  range: ReportRange
  by: 'host' | 'group'
  from: string
  to: string
  group_id: string | null
  fleet: ReportFleet
  items: AvailabilityItem[] | AvailabilityGroupItem[]
}

export interface ServiceCheckItem extends ReportFleet {
  host_id: string
  hostname: string
  group_id: string | null
  group_name: string | null
  check_id: string
  check_name: string
  warn_s: number
  tracked_from: string
}

export interface ServiceChecksReport {
  range: ReportRange
  from: string
  to: string
  group_id: string | null
  fleet: ReportFleet & { warn_s: number }
  items: ServiceCheckItem[]
}
