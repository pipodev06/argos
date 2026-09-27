import { ApiError, apiRequest } from '@/lib/api'
import { filenameFromDisposition } from '@/features/hosts/utils/history'
import type { Group, GroupInput, GroupListResponse, GroupUsage } from '../types/group.types'

export function listGroups(signal?: AbortSignal): Promise<GroupListResponse> {
  return apiRequest<GroupListResponse>('/groups', { signal })
}

export async function createGroup(input: GroupInput): Promise<Group> {
  const data = await apiRequest<{ group: Group }>('/groups', { method: 'POST', body: input })
  return data.group
}

export async function updateGroup(id: string, input: GroupInput): Promise<Group> {
  const data = await apiRequest<{ group: Group }>(`/groups/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: input,
  })
  return data.group
}

export function deleteGroup(id: string): Promise<unknown> {
  return apiRequest<unknown>(`/groups/${encodeURIComponent(id)}`, { method: 'DELETE', noData: true })
}

/** Consumo actual de cada recurso con cuota del cliente (B7). */
export function getGroupUsage(id: string, signal?: AbortSignal): Promise<GroupUsage> {
  return apiRequest<GroupUsage>(`/groups/${encodeURIComponent(id)}/usage`, { signal })
}

/** Descarga el respaldo del cliente (hosts + disponibilidad, zip) antes de darlo de baja. */
export async function downloadGroupExport(id: string, groupName: string): Promise<void> {
  let res: Response
  try {
    res = await fetch(`/api/v1/groups/${encodeURIComponent(id)}/export.zip`, { credentials: 'same-origin' })
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
    throw new ApiError(code, message, res.status)
  }
  const blob = await res.blob()
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filenameFromDisposition(res.headers.get('Content-Disposition')) ?? `cliente-${groupName}.zip`
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}
