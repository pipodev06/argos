import { apiRequest } from '@/lib/api'
import type {
  Alert,
  AlertDetailResponse,
  AlertListResponse,
  AlertRule,
  AlertSummary,
  CreateRuleInput,
  HostEvent,
  MaintenanceInput,
  MaintenanceWindow,
  RuleInput,
} from '../types/alert.types'

const enc = encodeURIComponent

export function listAlerts(params: URLSearchParams, signal?: AbortSignal): Promise<AlertListResponse> {
  return apiRequest<AlertListResponse>(`/alerts?${params.toString()}`, { signal })
}

export function getAlert(id: string, signal?: AbortSignal): Promise<AlertDetailResponse> {
  return apiRequest<AlertDetailResponse>(`/alerts/${enc(id)}`, { signal })
}

export function getAlertSummary(signal?: AbortSignal): Promise<AlertSummary> {
  return apiRequest<AlertSummary>('/alerts/summary', { signal })
}

export async function alertAction(id: string, action: 'ack' | 'resolve'): Promise<Alert> {
  const data = await apiRequest<{ alert: Alert }>(`/alerts/${enc(id)}/${action}`, { method: 'POST' })
  return data.alert
}

export async function listRules(signal?: AbortSignal): Promise<AlertRule[]> {
  const data = await apiRequest<{ items: AlertRule[] }>('/alert-rules', { signal })
  return data.items
}

export async function createRule(input: CreateRuleInput): Promise<AlertRule> {
  const data = await apiRequest<{ rule: AlertRule }>('/alert-rules', { method: 'POST', body: input })
  return data.rule
}

/** PATCH parcial. `kind` y `metric` no se envían nunca (el servidor los rechaza). */
export async function updateRule(id: string, input: Partial<RuleInput>): Promise<AlertRule> {
  const data = await apiRequest<{ rule: AlertRule }>(`/alert-rules/${enc(id)}`, { method: 'PATCH', body: input })
  return data.rule
}

/** Reemplaza los canales asignados a la regla (lista vacía = ninguno). */
export async function setRuleChannels(id: string, channelIds: string[]): Promise<string[]> {
  const data = await apiRequest<{ channel_ids: string[] }>(`/alert-rules/${enc(id)}/channels`, {
    method: 'PUT',
    body: { channel_ids: channelIds },
  })
  return data.channel_ids
}

export function deleteRule(id: string): Promise<unknown> {
  return apiRequest<unknown>(`/alert-rules/${enc(id)}`, { method: 'DELETE', noData: true })
}

export async function listMaintenance(signal?: AbortSignal): Promise<MaintenanceWindow[]> {
  const data = await apiRequest<{ items: MaintenanceWindow[] }>('/maintenance', { signal })
  return data.items
}

export async function createMaintenance(input: MaintenanceInput): Promise<MaintenanceWindow> {
  const data = await apiRequest<{ maintenance: MaintenanceWindow }>('/maintenance', { method: 'POST', body: input })
  return data.maintenance
}

export async function updateMaintenance(id: string, input: Partial<MaintenanceInput>): Promise<MaintenanceWindow> {
  const data = await apiRequest<{ maintenance: MaintenanceWindow }>(`/maintenance/${enc(id)}`, {
    method: 'PATCH',
    body: input,
  })
  return data.maintenance
}

export function deleteMaintenance(id: string): Promise<unknown> {
  return apiRequest<unknown>(`/maintenance/${enc(id)}`, { method: 'DELETE', noData: true })
}

export async function listHostEvents(hostId: string, signal?: AbortSignal): Promise<HostEvent[]> {
  const data = await apiRequest<{ items: HostEvent[] }>(`/hosts/${enc(hostId)}/events?limit=50`, { signal })
  return data.items
}
