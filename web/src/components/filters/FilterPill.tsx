import { useState, type ReactNode } from 'react'
import { ChevronDownIcon, XIcon } from 'lucide-react'
import { cn } from 'cn'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'

/** Base visual de toda pastilla de filtro: ancho fijo w-40, alto 8, radio 6px. */
export const PILL =
  'inline-flex h-8 w-40 items-center rounded-md border border-text-muted text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring'

interface FilterPillProps {
  label: string
  /** Valor resumido; null = pastilla inactiva. */
  summary: string | null
  onClear: () => void
  /** Se llama al abrir/cerrar (p. ej. para reiniciar pasos internos). */
  onOpenChange?: (open: boolean) => void
  /** Clases extra del popover (p. ej. mas ancho). */
  contentClassName?: string
  children: (close: () => void) => ReactNode
}

/** Pastilla con popover: inactiva muestra la etiqueta; activa, el valor y un boton para quitarla. */
export function FilterPill({ label, summary, onClear, onOpenChange, contentClassName, children }: FilterPillProps) {
  const [open, setOpen] = useState(false)
  const active = summary !== null
  function change(next: boolean) {
    setOpen(next)
    onOpenChange?.(next)
  }
  return (
    <Popover open={open} onOpenChange={change}>
      <div
        className={`inline-flex h-8 w-40 items-stretch overflow-hidden rounded-md border border-text-muted text-sm ${active ? 'bg-muted' : ''}`}
      >
        <PopoverTrigger asChild>
          <button
            type="button"
            title={summary ?? undefined}
            aria-label={active ? `${label}: ${summary}` : label}
            className="flex min-w-0 flex-1 items-center gap-1 px-3 text-left outline-none hover:bg-muted focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
          >
            <span className={`min-w-0 flex-1 truncate ${active ? 'font-medium' : ''}`}>{summary ?? label}</span>
            {!active && <ChevronDownIcon className="size-4 shrink-0 text-text-muted" strokeWidth={1.5} aria-hidden />}
          </button>
        </PopoverTrigger>
        {active && (
          <button
            type="button"
            aria-label={`Quitar filtro ${label}`}
            onClick={onClear}
            className="inline-flex w-8 items-center justify-center border-l border-text-muted text-text-muted outline-none hover:text-text focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
          >
            <XIcon className="size-4" strokeWidth={1.5} aria-hidden />
          </button>
        )}
      </div>
      <PopoverContent aria-label={label} className={cn('max-h-80 w-40 overflow-y-auto', contentClassName)}>
        {children(() => change(false))}
      </PopoverContent>
    </Popover>
  )
}

/** Pastilla quitable (sin popover) de ancho dinamico (segun su texto, hasta max-w-xs), para filtros activos. */
export function RemovableChip({ text, onRemove }: { text: string; onRemove: () => void }) {
  return (
    <li
      title={text}
      className="inline-flex h-8 w-fit max-w-xs items-stretch overflow-hidden rounded-md border border-text-muted bg-muted text-sm"
    >
      <span className="min-w-0 flex-1 self-center truncate px-3 font-medium">{text}</span>
      <button
        type="button"
        aria-label={`Quitar filtro ${text}`}
        onClick={onRemove}
        className="inline-flex w-8 items-center justify-center border-l border-text-muted text-text-muted outline-none hover:text-text focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
      >
        <XIcon className="size-4" strokeWidth={1.5} aria-hidden />
      </button>
    </li>
  )
}
