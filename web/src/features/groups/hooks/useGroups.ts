import { usePolledFetch } from '@/hooks/usePolledFetch'
import { listGroups } from '../services/groups.service'

const options = { intervalMs: 30_000 }

export function useGroups() {
  const { data, error, loading, refresh } = usePolledFetch(listGroups, options)
  return { groups: data, error, loading, refresh }
}
