import { Skeleton } from '@/components/ui/skeleton'
import { Row } from './DataTable'

/** Filas de carga (una celda que abarca todas las columnas). */
export function SkeletonRows({ rows = 8, colSpan }: { rows?: number; colSpan: number }) {
  return Array.from({ length: rows }, (_, i) => (
    <Row key={i} aria-hidden>
      <td colSpan={colSpan} className="h-9 px-3">
        <Skeleton className="h-4 w-full" />
      </td>
    </Row>
  ))
}
