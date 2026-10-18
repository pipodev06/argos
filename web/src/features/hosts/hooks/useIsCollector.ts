import { useCallback } from 'react'
import { listDevices } from '@/features/devices/services/devices.service'
import { isClientError, usePolledFetch } from '@/hooks/usePolledFetch'

const OPTIONS = { intervalMs: 60_000, stopOn: isClientError }

/** ¿Este host sondea dispositivos como colector? null = aun no se sabe. Una fila basta (limit=1). */
export function useIsCollector(hostId: string): boolean | null {
  const fetcher = useCallback(
    (signal: AbortSignal) => listDevices(new URLSearchParams({ collector: hostId, limit: '1' }), signal),
    [hostId],
  )
  const { data, error } = usePolledFetch(fetcher, OPTIONS)
  if (data) return data.items.length > 0
  return error ? false : null
}
