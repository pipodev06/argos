import { apiRequest } from '@/lib/api'
import type { RetentionSettings } from '../types/retention.types'

export function getRetention(signal?: AbortSignal): Promise<RetentionSettings> {
  return apiRequest<RetentionSettings>('/settings/retention', { signal })
}

export function putRetention(detailDays: number, confirmShrink: boolean): Promise<RetentionSettings> {
  return apiRequest<RetentionSettings>('/settings/retention', {
    method: 'PUT',
    body: { detail_days: detailDays, confirm_shrink: confirmShrink },
  })
}

/** `null` = el host hereda (del grupo o el global). */
export function putHostRetention(id: string, days: number | null, confirmShrink: boolean) {
  return apiRequest<{ detail_retention_days: number | null; effective_detail_days: number; retention_source: string }>(
    `/hosts/${encodeURIComponent(id)}/retention`,
    { method: 'PUT', body: { detail_retention_days: days, confirm_shrink: confirmShrink } },
  )
}
