import { apiRequest } from '@/lib/api'
import type { GroupPurgeOptions, PurgeJob } from '../types/purge.types'

// Las bajas responden 202 con el trabajo: la ejecucion sigue en el servidor y se consulta con getPurgeJob.
export async function purgeHost(id: string, confirmHostname: string): Promise<PurgeJob> {
  const data = await apiRequest<{ job: PurgeJob }>(`/hosts/${encodeURIComponent(id)}/purge`, {
    method: 'POST',
    body: { confirm_hostname: confirmHostname },
  })
  return data.job
}

export async function purgeGroup(id: string, confirmName: string, opts: GroupPurgeOptions): Promise<PurgeJob> {
  const data = await apiRequest<{ job: PurgeJob }>(`/groups/${encodeURIComponent(id)}/purge`, {
    method: 'POST',
    body: { confirm_name: confirmName, delete_group: opts.delete_group, disable_users: opts.disable_users },
  })
  return data.job
}

export async function getPurgeJob(id: string, signal?: AbortSignal): Promise<PurgeJob> {
  const data = await apiRequest<{ job: PurgeJob }>(`/purge-jobs/${encodeURIComponent(id)}`, { signal })
  return data.job
}
