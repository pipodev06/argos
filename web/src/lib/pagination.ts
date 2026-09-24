// Paginacion por cursor: el servidor solo dice cual es el cursor de la pagina siguiente, asi que "Anterior" se
// resuelve en el cliente con una pila de los cursores ya visitados. Funciones puras (sin React).

export const PAGE_SIZES = [25, 50, 100] as const
export type PageSize = (typeof PAGE_SIZES)[number]
export const DEFAULT_PAGE_SIZE: PageSize = 25

export function isPageSize(value: number): value is PageSize {
  return (PAGE_SIZES as readonly number[]).includes(value)
}

export interface CursorState {
  /** Cursor de la pagina actual (null = primera pagina). */
  cursor: string | null
  /** Cursores de las paginas anteriores, de la primera a la inmediata anterior. */
  stack: readonly (string | null)[]
}

export const INITIAL_CURSOR_STATE: CursorState = { cursor: null, stack: [] }

/** Avanza a la pagina cuyo cursor entrego el servidor. */
export function goNext(state: CursorState, nextCursor: string): CursorState {
  return { cursor: nextCursor, stack: [...state.stack, state.cursor] }
}

/** Vuelve a la pagina anterior; en la primera no hace nada. */
export function goPrev(state: CursorState): CursorState {
  if (state.stack.length === 0) return state
  const previous = state.stack[state.stack.length - 1]
  return { cursor: previous, stack: state.stack.slice(0, -1) }
}

/** Numero de pagina (desde 1). */
export function pageNumber(state: CursorState): number {
  return state.stack.length + 1
}

/** "26–50", "26–50 de 137" o "Sin resultados". `count` = filas de la pagina actual. */
export function formatPageRange(
  page: number,
  pageSize: number,
  count: number,
  total?: number | null,
  locale = 'es-PE',
): string {
  if (count <= 0) return 'Sin resultados'
  const start = (page - 1) * pageSize + 1
  const end = start + count - 1
  const fmt = (n: number) => n.toLocaleString(locale)
  const range = `${fmt(start)}–${fmt(end)}`
  return total == null ? range : `${range} de ${fmt(total)}`
}
