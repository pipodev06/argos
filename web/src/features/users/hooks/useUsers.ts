import { usePolledFetch } from '@/hooks/usePolledFetch'
import { listUsers } from '../services/users.service'

const options = { intervalMs: 30_000 }

export function useUsers() {
  const { data, error, loading, refresh } = usePolledFetch(listUsers, options)
  return { users: data?.items ?? null, error, loading, refresh }
}
