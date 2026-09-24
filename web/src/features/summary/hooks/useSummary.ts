import { usePolledFetch } from '@/hooks/usePolledFetch'
import { getSummary } from '../services/summary.service'

const options = { intervalMs: 10_000 }

export function useSummary() {
  return usePolledFetch(getSummary, options)
}
