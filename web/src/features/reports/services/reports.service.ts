import { apiRequest } from '@/lib/api'
import type { AvailabilityReport, ReportRange, ReportView, ServiceChecksReport } from '../types/report.types'

// Rango libre (B12): from/to en RFC3339, reemplazan a `range` cuando este es 'custom'. Van sueltos (no en un
// objeto) para que las dependencias de los hooks sean primitivas: un objeto nuevo cada render dispararia el
// fetch en bucle.
function rangeParams(range: ReportRange, from?: string, to?: string): URLSearchParams {
  if (from && to) return new URLSearchParams({ from, to })
  return new URLSearchParams({ range })
}

export async function getAvailabilityReport(
  range: ReportRange = '30d',
  by: ReportView = 'host',
  groupId?: string,
  from?: string,
  to?: string,
  signal?: AbortSignal,
): Promise<AvailabilityReport> {
  const params = rangeParams(range, from, to)
  params.append('by', by)
  if (groupId) params.append('group_id', groupId)
  return apiRequest<AvailabilityReport>(`/reports/availability?${params}`, { signal })
}

export async function getServiceChecksReport(
  range: ReportRange = '30d',
  groupId?: string,
  from?: string,
  to?: string,
  signal?: AbortSignal,
): Promise<ServiceChecksReport> {
  const params = rangeParams(range, from, to)
  if (groupId) params.append('group_id', groupId)
  return apiRequest<ServiceChecksReport>(`/reports/service-checks?${params}`, { signal })
}

export type ReportCsv = 'availability' | 'service-checks' | 'alerts'

/** URL de descarga de un CSV (el navegador la baja con `<a download>`; requiere la sesion). */
export function reportCsvUrl(kind: ReportCsv, range: ReportRange, groupId?: string, from?: string, to?: string): string {
  const params = rangeParams(range, from, to)
  if (groupId) params.append('group_id', groupId)
  return `/api/v1/reports/${kind}.csv?${params}`
}
