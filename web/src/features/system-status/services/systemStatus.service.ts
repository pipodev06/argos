import { apiRequest } from '@/lib/api'
import type { SystemStatus } from '../types/systemStatus.types'

export function getSystemStatus(signal?: AbortSignal): Promise<SystemStatus> {
  return apiRequest<SystemStatus>('/system/status', { signal })
}
