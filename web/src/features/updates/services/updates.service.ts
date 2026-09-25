import { apiRequest } from '@/lib/api'
import type { FleetPage, HostUpdatesResponse } from '../types/updates.types'

export function getHostUpdates(hostId: string, history: number, signal?: AbortSignal): Promise<HostUpdatesResponse> {
  return apiRequest<HostUpdatesResponse>(`/hosts/${encodeURIComponent(hostId)}/updates?history=${history}`, { signal })
}

export function listFleetUpdates(query: URLSearchParams, signal?: AbortSignal): Promise<FleetPage> {
  return apiRequest<FleetPage>(`/updates?${query.toString()}`, { signal })
}
