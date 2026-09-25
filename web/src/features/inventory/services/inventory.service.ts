import { apiRequest } from '@/lib/api'
import type { ChangesPage, InventoryModule, InventorySummary, ModuleList, PortItem, ProcessItem, ServiceItem } from '../types/inventory.types'

const enc = encodeURIComponent

export interface ItemsByModule {
  ports: PortItem
  services: ServiceItem
  processes: ProcessItem
}

export function getInventoryModule<M extends InventoryModule>(
  hostId: string,
  module: M,
  signal?: AbortSignal,
): Promise<ModuleList<ItemsByModule[M]>> {
  return apiRequest<ModuleList<ItemsByModule[M]>>(`/hosts/${enc(hostId)}/inventory/${module}`, { signal })
}

export function listInventoryChanges(hostId: string, query: URLSearchParams, signal?: AbortSignal): Promise<ChangesPage> {
  return apiRequest<ChangesPage>(`/hosts/${enc(hostId)}/inventory/changes?${query.toString()}`, { signal })
}

export function getInventorySummary(hostId: string, signal?: AbortSignal): Promise<InventorySummary> {
  return apiRequest<InventorySummary>(`/hosts/${enc(hostId)}/inventory`, { signal })
}
