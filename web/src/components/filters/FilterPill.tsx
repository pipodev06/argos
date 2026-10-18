import { useState, type ReactNode } from 'react'
import { CirclePlusIcon, XIcon } from 'lucide-react'
import { cn } from 'cn'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { ITEM } from './styles'

/** Base de todo boton de filtro (patron "faceted filter" de la tabla de shadcn): borde punteado, alto 8, ancho segun texto. */
export const PILL =
  'inline-flex h-8 shrink-0 items-center gap-1.5 rounded-md border border-dashed border-text-muted px-2 text-sm outline-none hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring'

/** Valor elegido dentro del boton de filtro. */
export function PillValue({ children }: { children: ReactNode }) {
  return (
    <>
      <span aria-hidden className="h-4 w-px bg-border" />
      <span className="max-w-40 truncate rounded-sm bg-muted px-1 text-xs font-medium">{children}</span>
    </>
  )
}

interface FilterPillProps {
  label: string
  /** Valor resumido; null = filtro inactivo. */
  summary: string | null
  onClear: () => void
  /** Se llama al abrir/cerrar (p. ej. para reiniciar pasos internos). */
  onOpenChange?: (open: boolean) => void
  /** Clases extra del popover (p. ej. mas ancho). */
  contentClassName?: string
  children: (close: () => void) => ReactNode
}

/** Boton de filtro con popover: "+ Etiqueta"; activo agrega el valor y, en el popover, "Quitar filtro". */
export function FilterPill({ label, summary, onClear, onOpenChange, contentClassName, children }: FilterPillProps) {
  const [open, setOpen] = useState(false)
  function change(next: boolean) {
    setOpen(next)
    onOpenChange?.(next)
  }
  return (
    <Popover open={open} onOpenChange={change}>
      <PopoverTrigger asChild>
        <button type="button" title={summary ?? undefined} aria-label={summary ? `${label}: ${summary}` : label} className={PILL}>
          <CirclePlusIcon className="size-4 text-text-muted" strokeWidth={1.5} aria-hidden />
          {label}
          {summary && <PillValue>{summary}</PillValue>}
        </button>
      </PopoverTrigger>
      <PopoverContent aria-label={label} align="start" className={cn('max-h-80 w-48 overflow-y-auto', contentClassName)}>
        {children(() => change(false))}
        {summary && (
          <>
            <div aria-hidden className="-mx-1 my-1 h-px bg-border" />
            <button
              type="button"
              className={`${ITEM} justify-center`}
              onClick={() => {
                onClear()
                change(false)
              }}
            >
              Quitar filtro
            </button>
          </>
        )}
      </PopoverContent>
    </Popover>
  )
}

/** Filtro activo de "Más filtros", quitable, en la misma fila. */
export function RemovableChip({ text, onRemove }: { text: string; onRemove: () => void }) {
  return (
    <li title={text} className="inline-flex h-8 max-w-xs items-center gap-1 rounded-md bg-muted pr-1 pl-2 text-sm font-medium">
      <span className="min-w-0 truncate">{text}</span>
      <button
        type="button"
        aria-label={`Quitar filtro ${text}`}
        onClick={onRemove}
        className="inline-flex size-6 items-center justify-center rounded-sm text-text-muted outline-none hover:text-text focus-visible:ring-2 focus-visible:ring-ring"
      >
        <XIcon className="size-4" strokeWidth={1.5} aria-hidden />
      </button>
    </li>
  )
}
