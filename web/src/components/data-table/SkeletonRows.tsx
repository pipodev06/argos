import { Skeleton } from '@/components/ui/skeleton'
import { Row } from './DataTable'

/** Filas de carga (una celda que abarca todas las columnas). */
export function SkeletonRows({ rows = 8, colSpan }: { rows?: number; colSpan: number }) {
  return Array.from({ length: rows }, (_, i) => (
    <Row key={i} aria-hidden>
      <td colSpan={colSpan} className="h-8 px-2 py-1">
        <Skeleton className="h-4 w-full" />
      </td>
    </Row>
  ))
}
