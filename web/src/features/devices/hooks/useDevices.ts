import { useCallback } from 'react'
import { isClientError, usePolledFetch } from '@/hooks/usePolledFetch'
import { getDevice, listAllDevices, listCollectors, listDevices } from '../services/devices.service'

const REFRESH_MS = 30_000

/** Una pagina de dispositivos; `qs` (limit + cursor serializados) es la identidad de la consulta. */
export function useDeviceList(qs: string) {
  const fetcher = useCallback((signal: AbortSignal) => listDevices(new URLSearchParams(qs), signal), [qs])
  return usePolledFetch(fetcher, { intervalMs: REFRESH_MS, stopOn: isClientError })
}

export function useDeviceDetail(id: string) {
  const fetcher = useCallback((signal: AbortSignal) => getDevice(id, signal), [id])
  return usePolledFetch(fetcher, { intervalMs: REFRESH_MS, stopOn: isClientError })
}

// Listas de opciones de los formularios (solo admin): una carga al montar, sin refresco.
const ONCE = { intervalMs: REFRESH_MS, paused: true } as const

export function useAllDevices() {
  return usePolledFetch(listAllDevices, ONCE)
}

export function useCollectors() {
  return usePolledFetch(listCollectors, ONCE)
}
