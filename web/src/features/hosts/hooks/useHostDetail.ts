import { useCallback } from 'react'
import { isClientError, usePolledFetch } from '@/hooks/usePolledFetch'
import { getHost } from '../services/hosts.service'

const REFRESH_MS = 30_000
const options = { intervalMs: REFRESH_MS, stopOn: isClientError }

export function useHostDetail(id: string) {
  const fetcher = useCallback((signal: AbortSignal) => getHost(id, signal), [id])
  return usePolledFetch(fetcher, options)
}
