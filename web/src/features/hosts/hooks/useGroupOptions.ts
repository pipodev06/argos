import type { FilterOption } from '@/components/filters/types'
import { useGroups } from '@/features/groups/hooks/useGroups'

/** Opciones del filtro "Grupo": "Sin grupo" (id "none") + los grupos existentes. */
export function useGroupOptions(): readonly FilterOption[] {
  const { groups } = useGroups()
  return [{ id: 'none', name: 'Sin grupo' }, ...(groups?.items ?? []).map((g) => ({ id: g.id, name: g.name }))]
}
