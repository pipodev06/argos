import { useMemo } from 'react'
import { useSearchParams } from 'react-router'
import type { FilterField, FiltersOf } from '@/components/filters/types'
import { clearedFilters, hasActiveFilters, parseFilters, serializeFilters } from '@/components/filters/urlState'

interface Options {
  /** Se llama tras cada cambio de filtros (p. ej. volver a la primera página). */
  onChange?: () => void
}

/**
 * Filtros de una tabla en la URL. `fields` debe ser una constante de módulo (identidad estable).
 * Los cambios que solo tocan búsquedas reemplazan la entrada del historial (no una por tecla).
 */
export function useUrlFilters<F extends readonly FilterField[]>(fields: F, { onChange }: Options = {}) {
  const [params, setParams] = useSearchParams()
  const filters = useMemo(() => parseFilters(fields, params), [fields, params])

  function write(next: FiltersOf<F>, replace: boolean) {
    setParams(serializeFilters(fields, next), { replace })
    onChange?.()
  }

  function setFilters(patch: Partial<FiltersOf<F>>) {
    const keys = Object.keys(patch)
    const replace = keys.length > 0 && keys.every((k) => fields.some((f) => f.key === k && f.kind === 'search'))
    write({ ...filters, ...patch }, replace)
  }

  return {
    filters,
    setFilters,
    clear: () => write(clearedFilters(fields, filters), false),
    hasActive: hasActiveFilters(fields, filters),
  }
}
