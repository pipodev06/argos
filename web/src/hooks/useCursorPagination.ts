import { useCallback, useState } from 'react'
import {
  DEFAULT_PAGE_SIZE,
  INITIAL_CURSOR_STATE,
  goNext,
  goPrev,
  pageNumber,
  type CursorState,
  type PageSize,
} from '@/lib/pagination'

/**
 * Estado de paginacion por cursor: cursor actual, tamano de pagina y pila de cursores previos.
 * `hasNext` no vive aqui: lo decide la respuesta del servidor (si trae cursor siguiente).
 * Cambiar el tamano o llamar a `reset` (p. ej. al cambiar filtros) vuelve a la primera pagina.
 */
export function useCursorPagination(initialPageSize: PageSize = DEFAULT_PAGE_SIZE) {
  const [pageSize, setPageSizeState] = useState<PageSize>(initialPageSize)
  const [state, setState] = useState<CursorState>(INITIAL_CURSOR_STATE)

  const next = useCallback((nextCursor: string) => setState((s) => goNext(s, nextCursor)), [])
  const prev = useCallback(() => setState(goPrev), [])
  const reset = useCallback(() => setState(INITIAL_CURSOR_STATE), [])
  const setPageSize = useCallback((size: PageSize) => {
    setPageSizeState(size)
    setState(INITIAL_CURSOR_STATE)
  }, [])

  return {
    cursor: state.cursor,
    page: pageNumber(state),
    hasPrev: state.stack.length > 0,
    pageSize,
    next,
    prev,
    reset,
    setPageSize,
  }
}
