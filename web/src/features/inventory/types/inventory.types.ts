export type InventoryModule = 'ports' | 'services' | 'processes'
type ChangeModule = 'ports' | 'services'
export type ChangeKind = 'added' | 'removed' | 'changed' | 'overflow'
export type ModuleStatus = 'ok' | 'no_data' | 'disabled'

export interface PortItem {
  proto: string
  bind: string
  port: number
  proc: string
  owner_known: boolean
}

export type ServiceState = 'running' | 'stopped' | 'paused' | 'other'
export type ServiceStart = 'auto' | 'manual' | 'disabled' | 'other'

export interface ServiceItem {
  name: string
  display: string
  state: ServiceState
  start: ServiceStart
}

export interface ProcessItem {
  name: string
  count: number
  rss_bytes: number
}

export interface ModuleList<T> {
  module: InventoryModule
  status: ModuleStatus
  count: number
  truncated: boolean
  collected_at: string | null
  updated_at: string | null
  items: T[]
}

export interface InventoryChange {
  id: string
  module: ChangeModule
  kind: ChangeKind
  key: string
  before: unknown
  after: unknown
  at: string
}

export interface ChangesPage {
  items: InventoryChange[]
  next_cursor: string | null
}

