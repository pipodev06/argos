import { apiRequest } from '@/lib/api'
import type { SnmpProfile, SnmpProfileInput } from '../types/snmpProfile.types'

const enc = encodeURIComponent

export async function listSnmpProfiles(signal?: AbortSignal): Promise<SnmpProfile[]> {
  const data = await apiRequest<{ items: SnmpProfile[] }>('/snmp-profiles', { signal })
  return data.items
}

export async function createSnmpProfile(input: SnmpProfileInput): Promise<SnmpProfile> {
  const data = await apiRequest<{ profile: SnmpProfile }>('/snmp-profiles', { method: 'POST', body: input })
  return data.profile
}

export async function updateSnmpProfile(id: string, input: SnmpProfileInput): Promise<SnmpProfile> {
  const data = await apiRequest<{ profile: SnmpProfile }>(`/snmp-profiles/${enc(id)}`, { method: 'PATCH', body: input })
  return data.profile
}

export function deleteSnmpProfile(id: string): Promise<unknown> {
  return apiRequest<unknown>(`/snmp-profiles/${enc(id)}`, { method: 'DELETE', noData: true })
}
