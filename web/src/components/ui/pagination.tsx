import { IconChevronLeft, IconChevronRight } from "@tabler/icons-react"
import { Button } from "@/components/ui/button"
import { SegmentedControl } from "@/components/ui/segmented-control"
import { PAGE_SIZES, formatPageRange, isPageSize, type PageSize } from "@/lib/pagination"

const SIZE_OPTIONS = PAGE_SIZES.map((size) => ({ value: String(size), label: String(size) }))

interface PaginationProps {
  /** Pagina actual, desde 1. */
  page: number
  pageSize: PageSize
  /** Filas de la pagina actual. */
  count: number
  /** Total si el servidor lo informa; con cursores normalmente no. */
  total?: number | null
  /** Nombre plural de lo paginado, para el contador ("hosts"). */
  itemLabel: string
  hasPrev: boolean
  hasNext: boolean
  onPrev: () => void
  onNext: () => void
  onPageSizeChange: (size: PageSize) => void
  /** Mientras carga una pagina se bloquea la navegacion (evita doble salto). */
  loading?: boolean
}

/**
 * Paginacion por cursor: unico componente para todas las tablas/listas. Es puro; el estado (pila de cursores,
 * tamano) vive en `useCursorPagination` y el `hasNext` lo decide el servidor.
 */
function Pagination({
  page,
  pageSize,
  count,
  total,
  itemLabel,
  hasPrev,
  hasNext,
  onPrev,
  onNext,
  onPageSizeChange,
  loading = false,
}: PaginationProps) {
  const range = formatPageRange(page, pageSize, count, total)
  const counter = count > 0 ? `${range} ${itemLabel}` : range

  return (
    <nav aria-label="Paginación" className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
      <p aria-live="polite" aria-atomic="true" className="text-xs text-text-muted tabular-nums">
        {counter}
      </p>
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <div className="flex items-center gap-2">
          <span className="text-xs text-text-muted">Por página</span>
          <SegmentedControl
            aria-label="Filas por página"
            options={SIZE_OPTIONS}
            value={String(pageSize)}
            onChange={(value) => {
              const size = Number(value)
              if (isPageSize(size)) onPageSizeChange(size)
            }}
          />
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={onPrev} disabled={!hasPrev || loading}>
            <IconChevronLeft stroke={1.5} aria-hidden />
            Anterior
          </Button>
          <Button variant="outline" size="sm" onClick={onNext} disabled={!hasNext || loading}>
            Siguiente
            <IconChevronRight stroke={1.5} aria-hidden />
          </Button>
        </div>
      </div>
    </nav>
  )
}

export { Pagination }
