import { apiRequest } from '@/lib/api'
import type { CreateUserInput, ManagedUser, UpdateUserInput, UserListResponse } from '../types/user.types'

export function listUsers(signal?: AbortSignal): Promise<UserListResponse> {
  return apiRequest<UserListResponse>('/users', { signal })
}

export async function createUser(input: CreateUserInput): Promise<ManagedUser> {
  const data = await apiRequest<{ user: ManagedUser }>('/users', { method: 'POST', body: input })
  return data.user
}

export async function updateUser(id: string, input: UpdateUserInput): Promise<ManagedUser> {
  const data = await apiRequest<{ user: ManagedUser }>(`/users/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: input,
  })
  return data.user
}

export async function deleteUser(id: string): Promise<void> {
  await apiRequest<unknown>(`/users/${encodeURIComponent(id)}`, { method: 'DELETE', noData: true })
}
