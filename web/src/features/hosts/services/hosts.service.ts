import { apiRequest } from '@/lib/api'
import type {
  BulkRequest,
  EnrollToken,
  GroupRef,
  HostDetailResponse,
  HostListResponse,
  HostMetrics,
  MetricRange,
  TagCount,
} from '../types/host.types'

export function listHosts(query: URLSearchParams, signal?: AbortSignal): Promise<HostListResponse> {
  return apiRequest<HostListResponse>(`/hosts?${query.toString()}`, { signal })
}

export function createEnrollToken(hostname: string, groupId: string | null): Promise<EnrollToken> {
  return apiRequest<EnrollToken>('/hosts/enroll-token', {
    method: 'POST',
    body: groupId ? { hostname, group_id: groupId } : { hostname },
  })
}

export function getHost(id: string, signal?: AbortSignal): Promise<HostDetailResponse> {
  return apiRequest<HostDetailResponse>(`/hosts/${encodeURIComponent(id)}`, { signal })
}

export function getHostMetrics(
  id: string,
  range: MetricRange,
  signal?: AbortSignal,
): Promise<HostMetrics> {
  return apiRequest<HostMetrics>(`/hosts/${encodeURIComponent(id)}/metrics?range=${range}`, {
    signal,
  })
}

export function setHostGroup(id: string, groupId: string | null) {
  return apiRequest<{ group: GroupRef | null }>(`/hosts/${encodeURIComponent(id)}/group`, {
    method: 'PUT',
    body: { group_id: groupId },
  })
}

export function setHostTags(id: string, tags: string[]) {
  return apiRequest<{ tags: string[] }>(`/hosts/${encodeURIComponent(id)}/tags`, {
    method: 'PUT',
    body: { tags },
  })
}

export function bulkHosts(request: BulkRequest) {
  return apiRequest<{ affected: number }>('/hosts/bulk', { method: 'POST', body: request })
}

export async function listTags(signal?: AbortSignal): Promise<TagCount[]> {
  const data = await apiRequest<{ tags: TagCount[] }>('/tags', { signal })
  return data.tags
}
