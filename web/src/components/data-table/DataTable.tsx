import type { ComponentProps, ReactNode } from 'react'
import { cn } from 'cn'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { CELL, HEAD, ROW } from './styles'

// Tabla unica de la app sobre la `Table` de shadcn: este modulo solo fija medidas y semantica (caption, scope).
export { TableBody, TableHeader }

interface DataTableProps {
  /** Nombre accesible de la tabla (caption sr-only). */
  caption: string
  className?: string
  children: ReactNode
}

/** Contenedor con borde + scroll horizontal (el de shadcn) + tabla. `className` va a la tabla (p. ej. `min-w-[40rem]`). */
export function DataTable({ caption, className, children }: DataTableProps) {
  return (
    <div className="rounded-md border border-border bg-surface">
      <Table className={cn('border-collapse', className)}>
        <caption className="sr-only">{caption}</caption>
        {children}
      </Table>
    </div>
  )
}

export function HeadRow({ className, ...props }: ComponentProps<'tr'>) {
  return <TableRow className={cn('border-b border-border hover:bg-transparent', className)} {...props} />
}

export function HeadCell({ className, ...props }: ComponentProps<'th'>) {
  return <TableHead scope="col" className={cn(HEAD, className)} {...props} />
}

export function Row({ className, ...props }: ComponentProps<'tr'>) {
  return <TableRow className={cn(ROW, className)} {...props} />
}

export function Cell({ className, ...props }: ComponentProps<'td'>) {
  return <TableCell className={cn(CELL, className)} {...props} />
}

/** Celda de encabezado de fila (nombre de la fila). */
export function RowHeadCell({ className, ...props }: ComponentProps<'th'>) {
  return <th scope="row" className={cn(CELL, 'align-middle font-semibold whitespace-nowrap', className)} {...props} />
}

/** Primera columna `#` (numero de fila); ancho fijo estrecho. Va con `RowNumberCell`. */
export function RowNumberHeadCell({ className }: { className?: string }) {
  return (
    <HeadCell className={cn('w-12', className)}>
      <span aria-hidden>#</span>
      <span className="sr-only">Número de fila</span>
    </HeadCell>
  )
}

/** Celda `#`: `n` ya calculado con `rowNumber(page, pageSize, index)`. */
export function RowNumberCell({ n, className }: { n: number; className?: string }) {
  return <Cell className={cn('w-12 text-xs text-text-muted tabular-nums', className)}>{n}</Cell>
}
