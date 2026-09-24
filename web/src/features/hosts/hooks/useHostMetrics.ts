import { useCallback } from 'react'
import { isClientError, usePolledFetch } from '@/hooks/usePolledFetch'
import { getHostMetrics } from '../services/hosts.service'
import type { MetricRange } from '../types/host.types'
import { getRange } from '../utils/chart'

export function useHostMetrics(id: string, range: MetricRange) {
  const fetcher = useCallback(
    (signal: AbortSignal) => getHostMetrics(id, range, signal),
    [id, range],
  )
  return usePolledFetch(fetcher, { intervalMs: getRange(range).refreshMs, stopOn: isClientError })
}
