import { apiRequest } from '@/lib/api'
import type { Group, GroupInput, GroupListResponse } from '../types/group.types'

export function listGroups(signal?: AbortSignal): Promise<GroupListResponse> {
  return apiRequest<GroupListResponse>('/groups', { signal })
}

export async function createGroup(input: GroupInput): Promise<Group> {
  const data = await apiRequest<{ group: Group }>('/groups', { method: 'POST', body: input })
  return data.group
}

export async function updateGroup(id: string, input: GroupInput): Promise<Group> {
  const data = await apiRequest<{ group: Group }>(`/groups/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: input,
  })
  return data.group
}

export function deleteGroup(id: string): Promise<unknown> {
  return apiRequest<unknown>(`/groups/${encodeURIComponent(id)}`, { method: 'DELETE', noData: true })
}
