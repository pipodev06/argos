import { apiRequest } from '@/lib/api'
import type { ChangePasswordPayload, LoginCredentials, User } from '../types/auth.types'

export async function login(credentials: LoginCredentials): Promise<User> {
  const data = await apiRequest<{ user: User }>('/auth/login', {
    method: 'POST',
    body: credentials,
  })
  return data.user
}

export function logout(): Promise<void> {
  return apiRequest<void>('/auth/logout', { method: 'POST', noData: true })
}

export async function fetchMe(signal?: AbortSignal): Promise<User> {
  const data = await apiRequest<{ user: User }>('/auth/me', { signal })
  return data.user
}

export function changePassword(payload: ChangePasswordPayload): Promise<void> {
  return apiRequest<void>('/auth/password', { method: 'POST', body: payload, noData: true })
}
