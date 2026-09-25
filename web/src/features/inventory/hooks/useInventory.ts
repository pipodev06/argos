import { useCallback } from 'react'
import { isClientError, usePolledFetch } from '@/hooks/usePolledFetch'
import { getInventoryModule, listInventoryChanges } from '../services/inventory.service'
import type { InventoryModule } from '../types/inventory.types'

// El inventario cambia poco: refresco cada 60 s (pausado con la pestana oculta), solo de la sub-seccion activa.
const REFRESH_MS = 60_000

export function useInventoryModule<M extends InventoryModule>(hostId: string, module: M) {
  const fetcher = useCallback((signal: AbortSignal) => getInventoryModule(hostId, module, signal), [hostId, module])
  return usePolledFetch(fetcher, { intervalMs: REFRESH_MS, stopOn: isClientError })
}

/** Cambios: sin refresco automatico (paginado por cursor); solo carga y `refresh()`. */
export function useInventoryChanges(hostId: string, module: string, kind: string, cursor: string | null, limit: number) {
  const params = new URLSearchParams({ limit: String(limit) })
  if (module !== 'all') params.set('module', module)
  if (kind !== 'all') params.set('kind', kind)
  if (cursor) params.set('cursor', cursor)
  const qs = params.toString()
  const fetcher = useCallback(
    (signal: AbortSignal) => listInventoryChanges(hostId, new URLSearchParams(qs), signal),
    [hostId, qs],
  )
  return usePolledFetch(fetcher, { intervalMs: REFRESH_MS, paused: true })
}
