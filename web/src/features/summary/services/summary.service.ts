import { apiRequest } from '@/lib/api'
import type { Summary } from '../types/summary.types'

export function getSummary(signal?: AbortSignal): Promise<Summary> {
  return apiRequest<Summary>('/summary', { signal })
}
