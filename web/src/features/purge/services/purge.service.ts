import { apiRequest } from '@/lib/api'
import type { DeletedRows, GroupPurgeOptions, GroupPurgeResult } from '../types/purge.types'

export function purgeHost(id: string, confirmHostname: string): Promise<{ deleted_rows: DeletedRows }> {
  return apiRequest(`/hosts/${encodeURIComponent(id)}/purge`, { method: 'POST', body: { confirm_hostname: confirmHostname } })
}

/** Operacion larga (puede tardar minutos): el llamador no debe reintentarla solo. */
export function purgeGroup(id: string, confirmName: string, opts: GroupPurgeOptions): Promise<GroupPurgeResult> {
  return apiRequest(`/groups/${encodeURIComponent(id)}/purge`, {
    method: 'POST',
    body: { confirm_name: confirmName, delete_group: opts.delete_group, disable_users: opts.disable_users },
  })
}
