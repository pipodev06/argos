import { useMemo } from 'react'
import type { FilterOption } from '@/components/filters/types'
import { useTags } from './useTags'

/** Etiquetas existentes como opciones de filtro (con conteo). */
export function useTagOptions(): readonly FilterOption[] {
  const tags = useTags()
  return useMemo(() => tags.map((t) => ({ id: t.name, name: t.name, count: t.count })), [tags])
}
