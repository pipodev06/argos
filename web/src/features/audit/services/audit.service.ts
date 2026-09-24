import { apiRequest } from '@/lib/api'
import type { AuditEventsResponse, AuditListResponse } from '../types/audit.types'

export function listAudit(query: URLSearchParams, signal?: AbortSignal): Promise<AuditListResponse> {
  return apiRequest<AuditListResponse>(`/audit?${query.toString()}`, { signal })
}

export function listAuditEvents(signal?: AbortSignal): Promise<AuditEventsResponse> {
  return apiRequest<AuditEventsResponse>('/audit/events', { signal })
}
