import type { ReactNode } from 'react'
import { EyeIcon } from 'lucide-react'
import { Link } from 'react-router'
import { cn } from 'cn'
import { Button } from '@/components/ui/button'
import { Cell, HeadCell } from './DataTable'

/** Encabezado visible de la columna de acciones (siempre la ultima). */
export function ActionsHeadCell({ className }: { className?: string }) {
  return <HeadCell className={cn('w-24', className)}>Acciones</HeadCell>
}

/** Celda de acciones: botones de icono en fila. */
export function ActionsCell({ children, className }: { children: ReactNode; className?: string }) {
  return <Cell className={cn('whitespace-nowrap', className)}>{children}</Cell>
}

/** Boton "ver detalle" (ojo): reemplaza al nombre como enlace; navega con `to`. */
export function ViewAction({ to, label }: { to: string; label: string }) {
  return (
    <Button asChild variant="ghost" size="icon-sm">
      <Link to={to} aria-label={`Ver ${label}`} title="Ver detalle">
        <EyeIcon strokeWidth={1.5} aria-hidden />
      </Link>
    </Button>
  )
}
