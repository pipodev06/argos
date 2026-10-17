import { useRef, type KeyboardEvent } from "react"
import { cn } from "cn"

interface SegmentedControlProps<T extends string> {
  /** `ariaLabel` (nombre accesible completo, tambien como `title`) es opcional: por defecto se usa `label`. */
  options: readonly { value: T; label: string; ariaLabel?: string }[]
  value: T
  onChange: (value: T) => void
  "aria-label": string
  className?: string
}

/**
 * Selector de una opcion entre pocas (radiogroup): tabulador entra al grupo, flechas cambian la
 * seleccion. La opcion activa se distingue por fondo + peso (no solo por color); `brand` solo en el foco.
 */
function SegmentedControl<T extends string>({
  options,
  value,
  onChange,
  className,
  "aria-label": ariaLabel,
}: SegmentedControlProps<T>) {
  const buttons = useRef<(HTMLButtonElement | null)[]>([])

  function handleKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    const index = options.findIndex((o) => o.value === value)
    let next = index
    if (event.key === "ArrowRight" || event.key === "ArrowDown") next = (index + 1) % options.length
    else if (event.key === "ArrowLeft" || event.key === "ArrowUp")
      next = (index - 1 + options.length) % options.length
    else if (event.key === "Home") next = 0
    else if (event.key === "End") next = options.length - 1
    else return
    event.preventDefault()
    onChange(options[next].value)
    buttons.current[next]?.focus()
  }

  return (
    <div
      role="radiogroup"
      aria-label={ariaLabel}
      onKeyDown={handleKeyDown}
      className={cn("inline-flex max-w-full overflow-x-auto rounded-md border border-border bg-surface p-0.5", className)}
    >
      {options.map((option, i) => {
        const checked = option.value === value
        return (
          <button
            key={option.value}
            ref={(el) => {
              buttons.current[i] = el
            }}
            type="button"
            role="radio"
            aria-checked={checked}
            aria-label={option.ariaLabel}
            title={option.ariaLabel}
            tabIndex={checked ? 0 : -1}
            onClick={() => onChange(option.value)}
            className={cn(
              "h-7 min-w-10 shrink-0 rounded-md px-2 py-1 text-sm outline-none transition-colors focus-visible:ring-2 focus-visible:ring-ring",
              checked ? "bg-border font-semibold text-text" : "text-text-muted hover:text-text"
            )}
          >
            {option.label}
          </button>
        )
      })}
    </div>
  )
}

export { SegmentedControl }
