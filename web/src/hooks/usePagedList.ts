import { useCallback, useMemo, useState } from 'react'
import { DEFAULT_PAGE_SIZE, pageWindow, type PageSize } from '@/lib/pagination'

/**
 * Paginacion en cliente de una lista completa. Devuelve las props que espera `Pagination` (con `total` conocido).
 * Cambiar el tamano vuelve a la pagina 1; para reiniciar al cambiar un filtro, remonta el componente con `key`.
 */
export function usePagedList<T>(items: readonly T[], initialPageSize: PageSize = DEFAULT_PAGE_SIZE) {
  const [requested, setRequested] = useState(1)
  const [pageSize, setPageSizeState] = useState<PageSize>(initialPageSize)
  const total = items.length
  const { page, start, end } = pageWindow(total, requested, pageSize)
  const rows = useMemo(() => items.slice(start, end), [items, start, end])

  const next = useCallback(() => setRequested(page + 1), [page])
  const prev = useCallback(() => setRequested(page - 1), [page])
  const setPageSize = useCallback((size: PageSize) => {
    setPageSizeState(size)
    setRequested(1)
  }, [])

  return { rows, total, page, pageSize, hasPrev: page > 1, hasNext: end < total, next, prev, setPageSize }
}
