import type { GroupRef, HostStatus } from '@/features/hosts/types/host.types'

export interface CollectorRef {
  id: string
  name: string
}

/** Dispositivo sin agente (GET /devices). `address` y `collector` solo los recibe un admin. */
export interface Device {
  id: string
  name: string
  kind: 'device'
  address?: string
  collector?: CollectorRef
  group: GroupRef | null
  status: HostStatus
  status_changed_at: string
  last_seen_at: string | null
  /** false = el colector no esta activo y el estado no se esta evaluando. */
  evaluated: boolean
  created_at: string
}

export interface DeviceListResponse {
  items: Device[]
  next_cursor: string | null
  total: number
}

/** Cuerpo de crear/editar. En PATCH `group_id: null` quita el grupo. */
export interface DeviceInput {
  name?: string
  address?: string
  collector_id?: string
  group_id?: string | null
}
