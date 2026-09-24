import { usePolledFetch } from '@/hooks/usePolledFetch'
import { listChannels } from '../services/channels.service'

export function useChannels() {
  return usePolledFetch(listChannels, { intervalMs: 30_000 })
}
