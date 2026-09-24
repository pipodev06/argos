import { apiRequest } from '@/lib/api'
import type { CheckEventsPage, CheckInput, HostCheck, ServiceCheck } from '../types/check.types'

const enc = encodeURIComponent

export async function listChecks(signal?: AbortSignal): Promise<ServiceCheck[]> {
  const data = await apiRequest<{ items: ServiceCheck[] }>('/service-checks', { signal })
  return data.items
}

export async function createCheck(input: CheckInput): Promise<ServiceCheck> {
  const data = await apiRequest<{ check: ServiceCheck }>('/service-checks', { method: 'POST', body: input })
  return data.check
}

export async function updateCheck(id: string, input: CheckInput): Promise<ServiceCheck> {
  const data = await apiRequest<{ check: ServiceCheck }>(`/service-checks/${enc(id)}`, { method: 'PATCH', body: input })
  return data.check
}

export function deleteCheck(id: string): Promise<unknown> {
  return apiRequest<unknown>(`/service-checks/${enc(id)}`, { method: 'DELETE', noData: true })
}

export async function listHostChecks(hostId: string, signal?: AbortSignal): Promise<HostCheck[]> {
  const data = await apiRequest<{ items: HostCheck[] }>(`/hosts/${enc(hostId)}/service-checks`, { signal })
  return data.items
}

export function listCheckEvents(hostId: string, checkId: string, cursor: string | null, signal?: AbortSignal): Promise<CheckEventsPage> {
  const qs = cursor ? `?cursor=${enc(cursor)}` : ''
  return apiRequest<CheckEventsPage>(`/hosts/${enc(hostId)}/service-checks/${enc(checkId)}/events${qs}`, { signal })
}
