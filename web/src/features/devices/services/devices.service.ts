import { listHosts } from '@/features/hosts/services/hosts.service'
import type { PurgeJob } from '@/features/purge/types/purge.types'
import { apiRequest } from '@/lib/api'
import type { CollectorRef, Device, DeviceInput, DeviceListResponse } from '../types/device.types'

const enc = encodeURIComponent

export function listDevices(query: URLSearchParams, signal?: AbortSignal): Promise<DeviceListResponse> {
  return apiRequest<DeviceListResponse>(`/devices?${query.toString()}`, { signal })
}

export async function getDevice(id: string, signal?: AbortSignal): Promise<Device> {
  const data = await apiRequest<{ device: Device }>(`/devices/${enc(id)}`, { signal })
  return data.device
}

export async function createDevice(input: DeviceInput): Promise<Device> {
  const data = await apiRequest<{ device: Device }>('/devices', { method: 'POST', body: input })
  return data.device
}

export async function updateDevice(id: string, input: DeviceInput): Promise<Device> {
  const data = await apiRequest<{ device: Device }>(`/devices/${enc(id)}`, { method: 'PATCH', body: input })
  return data.device
}

/** Baja en segundo plano: responde 202 con el trabajo (se consulta con `getPurgeJob`). */
export async function deleteDevice(id: string): Promise<PurgeJob> {
  const data = await apiRequest<{ job: PurgeJob }>(`/devices/${enc(id)}`, { method: 'DELETE' })
  return data.job
}

const MAX_OPTION_PAGES = 5

/** Todos los dispositivos (para un Select): paginas de 200 (maximo del servidor), con tope de seguridad. */
export async function listAllDevices(signal?: AbortSignal): Promise<Device[]> {
  const out: Device[] = []
  let cursor: string | null = null
  for (let i = 0; i < MAX_OPTION_PAGES; i++) {
    const q = new URLSearchParams({ limit: '200' })
    if (cursor) q.set('cursor', cursor)
    const page: DeviceListResponse = await listDevices(q, signal)
    out.push(...page.items)
    cursor = page.next_cursor
    if (!cursor) break
  }
  return out
}

/** Hosts CON agente (posibles colectores), por nombre; paginas de 100 (maximo del servidor), con tope de seguridad. */
export async function listCollectors(signal?: AbortSignal): Promise<CollectorRef[]> {
  const out: CollectorRef[] = []
  let cursor: string | null = null
  for (let i = 0; i < MAX_OPTION_PAGES; i++) {
    const q = new URLSearchParams({ sort: 'name', dir: 'asc', limit: '100' })
    if (cursor) q.set('cursor', cursor)
    const page = await listHosts(q, signal)
    for (const h of page.items) if (h.kind === 'agent' && h.status !== 'disabled') out.push({ id: h.id, name: h.hostname })
    cursor = page.next_cursor
    if (!cursor) break
  }
  return out
}
