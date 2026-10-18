import { useOutletContext } from 'react-router'
import type { useHostDetail } from './useHostDetail'

/** Lo que el layout del host comparte con sus sub-rutas (una sola consulta del detalle). */
export interface HostOutlet {
  hostId: string
  detail: ReturnType<typeof useHostDetail>
  /** El host sondea dispositivos; null = aun no se sabe. */
  isCollector: boolean | null
}

export function useHostOutlet(): HostOutlet {
  return useOutletContext<HostOutlet>()
}
