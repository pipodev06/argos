import type { ComponentProps, ReactNode } from 'react'
import { cn } from 'cn'
import { CELL, HEAD, ROW } from './styles'

interface DataTableProps {
  /** Nombre accesible de la tabla (caption sr-only). */
  caption: string
  className?: string
  children: ReactNode
}

/** Contenedor con borde + scroll horizontal + <table>. `className` va a la tabla (p. ej. `min-w-[40rem]`). */
export function DataTable({ caption, className, children }: DataTableProps) {
  return (
    <div className="overflow-x-auto rounded-md border border-border bg-surface">
      <table className={cn('w-full border-collapse', className)}>
        <caption className="sr-only">{caption}</caption>
        {children}
      </table>
    </div>
  )
}

export function HeadRow({ className, ...props }: ComponentProps<'tr'>) {
  return <tr className={cn('border-b border-border', className)} {...props} />
}

export function HeadCell({ className, ...props }: ComponentProps<'th'>) {
  return <th scope="col" className={cn(HEAD, className)} {...props} />
}

export function Row({ className, ...props }: ComponentProps<'tr'>) {
  return <tr className={cn(ROW, className)} {...props} />
}

export function Cell({ className, ...props }: ComponentProps<'td'>) {
  return <td className={cn(CELL, className)} {...props} />
}

/** Celda de encabezado de fila (nombre de la fila). */
export function RowHeadCell({ className, ...props }: ComponentProps<'th'>) {
  return <th scope="row" className={cn(CELL, 'font-semibold', className)} {...props} />
}
