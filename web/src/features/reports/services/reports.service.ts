import { apiRequest } from '@/lib/api'
import type { AvailabilityReport, ReportRange, ReportView, ServiceChecksReport } from '../types/report.types'

export async function getAvailabilityReport(
  range: ReportRange = '30d',
  by: ReportView = 'host',
  groupId?: string,
  signal?: AbortSignal,
): Promise<AvailabilityReport> {
  const params = new URLSearchParams({ range, by })
  if (groupId) params.append('group_id', groupId)
  return apiRequest<AvailabilityReport>(`/reports/availability?${params}`, { signal })
}

export async function getServiceChecksReport(
  range: ReportRange = '30d',
  groupId?: string,
  signal?: AbortSignal,
): Promise<ServiceChecksReport> {
  const params = new URLSearchParams({ range })
  if (groupId) params.append('group_id', groupId)
  return apiRequest<ServiceChecksReport>(`/reports/service-checks?${params}`, { signal })
}

export type ReportCsv = 'availability' | 'service-checks' | 'alerts'

/** URL de descarga de un CSV (el navegador la baja con `<a download>`; requiere la sesion). */
export function reportCsvUrl(kind: ReportCsv, range: ReportRange, groupId?: string): string {
  const params = new URLSearchParams({ range })
  if (groupId) params.append('group_id', groupId)
  return `/api/v1/reports/${kind}.csv?${params}`
}
