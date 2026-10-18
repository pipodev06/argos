import type { FilterOption } from '@/components/filters/types'
import { useCollectors } from './useDevices'

/** Opciones del filtro Colector (hosts con agente que sondean dispositivos). */
export function useCollectorOptions(): readonly FilterOption[] {
  const { data } = useCollectors()
  return (data ?? []).map((h) => ({ id: h.id, name: h.name }))
}
