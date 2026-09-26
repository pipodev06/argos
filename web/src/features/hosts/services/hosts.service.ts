import { ApiError, apiRequest } from '@/lib/api'
import {
  csvFilename,
  filenameFromDisposition,
  toUtcIso,
  type DateRange,
  type HistoryResponse,
  type HistorySeries,
} from '../utils/history'
import type {
  BulkRequest,
  EnrollToken,
  PendingEnrollToken,
  GroupRef,
  HostDetailResponse,
  HostListResponse,
  HostMetrics,
  MetricRange,
} from '../types/host.types'

export function listHosts(query: URLSearchParams, signal?: AbortSignal): Promise<HostListResponse> {
  return apiRequest<HostListResponse>(`/hosts?${query.toString()}`, { signal })
}

export function createEnrollToken(hostname: string, groupId: string | null): Promise<EnrollToken> {
  return apiRequest<EnrollToken>('/hosts/enroll-token', {
    method: 'POST',
    body: groupId ? { hostname, group_id: groupId } : { hostname },
  })
}

export async function listEnrollTokens(signal?: AbortSignal): Promise<PendingEnrollToken[]> {
  const data = await apiRequest<{ items: PendingEnrollToken[] }>('/hosts/enroll-tokens', { signal })
  return data.items
}

export function revokeEnrollToken(id: string): Promise<unknown> {
  return apiRequest<unknown>(`/hosts/enroll-tokens/${encodeURIComponent(id)}`, { method: 'DELETE', noData: true })
}

export function getHost(id: string, signal?: AbortSignal): Promise<HostDetailResponse> {
  return apiRequest<HostDetailResponse>(`/hosts/${encodeURIComponent(id)}`, { signal })
}

export function getHostMetrics(
  id: string,
  range: MetricRange,
  signal?: AbortSignal,
): Promise<HostMetrics> {
  return apiRequest<HostMetrics>(`/hosts/${encodeURIComponent(id)}/metrics?range=${range}`, {
    signal,
  })
}

export function setHostGroup(id: string, groupId: string | null) {
  return apiRequest<{ group: GroupRef | null }>(`/hosts/${encodeURIComponent(id)}/group`, {
    method: 'PUT',
    body: { group_id: groupId },
  })
}


export function renameHost(id: string, hostname: string) {
  return apiRequest<{ hostname: string }>(`/hosts/${encodeURIComponent(id)}/name`, {
    method: 'PUT',
    body: { hostname },
  })
}

export function bulkHosts(request: BulkRequest) {
  return apiRequest<{ affected: number }>('/hosts/bulk', { method: 'POST', body: request })
}


function historyQuery(range: DateRange, extra: Record<string, string> = {}): string {
  return new URLSearchParams({ from: toUtcIso(range.fromMs), to: toUtcIso(range.toMs), ...extra }).toString()
}

export function getHostHistory(id: string, range: DateRange, signal?: AbortSignal): Promise<HistoryResponse> {
  return apiRequest<HistoryResponse>(`/hosts/${encodeURIComponent(id)}/history?${historyQuery(range)}`, { signal })
}

/** Descarga el CSV (cookie de sesion) y lo entrega al navegador; los errores 400/429 salen como ApiError. */
export async function downloadHostHistoryCsv(
  id: string,
  hostname: string,
  range: DateRange,
  series: HistorySeries,
): Promise<void> {
  const path = `/api/v1/hosts/${encodeURIComponent(id)}/history?${historyQuery(range, { format: 'csv', series })}`
  let res: Response
  try {
    res = await fetch(path, { credentials: 'same-origin' })
  } catch {
    throw new ApiError('network_error', 'No se pudo conectar con el servidor')
  }
  if (!res.ok) {
    let code = 'bad_response'
    let message = `Respuesta inesperada del servidor (${res.status})`
    try {
      const body = (await res.json()) as { error?: { code: string; message: string } | null }
      if (body.error) ({ code, message } = body.error)
    } catch {
      // cuerpo no JSON: mensaje generico
    }
    const raw = res.headers.get('Retry-After')
    const retryAfter = raw && /^\d+$/.test(raw.trim()) ? Number(raw) : undefined
    throw new ApiError(code, message, res.status, retryAfter)
  }
  const blob = await res.blob()
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filenameFromDisposition(res.headers.get('Content-Disposition')) ?? csvFilename(series, hostname, range)
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}
