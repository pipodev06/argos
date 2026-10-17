import type { ReactNode } from "react"
import { cn } from "cn"
import type { LucideIcon } from "lucide-react"

interface StatCardProps {
  label: string
  Icon: LucideIcon
  /** Color del icono (p. ej. `text-ok`); por defecto hereda el muted de la etiqueta. */
  iconClassName?: string
  /** Barra de 2px `danger` a la izquierda (mismo patron que la fila de host caido). */
  alert?: boolean
  children: ReactNode
}

/** Tarjeta de ultimo valor: etiqueta con icono + contenido (uno o mas `StatValue`). */
function StatCard({ label, Icon, iconClassName, alert = false, children }: StatCardProps) {
  return (
    <section
      aria-label={label}
      className="relative flex min-h-24 flex-col gap-2 overflow-hidden rounded-md border border-border bg-surface p-4"
    >
      {alert && <span aria-hidden className="absolute inset-y-0 left-0 w-0.5 bg-danger" />}
      <h3 className="flex items-center gap-1.5 text-xs font-medium text-text-muted">
        <Icon className={cn("size-4 shrink-0", iconClassName)} strokeWidth={1.5} aria-hidden />
        {label}
      </h3>
      <div className="flex flex-col gap-1">{children}</div>
    </section>
  )
}

interface StatValueProps {
  /** null/undefined = sin dato: se muestra "sin datos". */
  children?: ReactNode
  size?: "lg" | "md"
  detail?: ReactNode
  /** Todavia cargando: muestra "Cargando…" en vez de "sin datos". */
  pending?: boolean
}

function StatValue({ children, size = "lg", detail, pending = false }: StatValueProps) {
  const empty = pending || children === null || children === undefined
  return (
    <div className="flex flex-wrap items-baseline gap-x-2 tabular-nums">
      <span
        className={cn(
          "font-semibold",
          size === "lg" ? "text-2xl" : "text-sm",
          empty && "text-sm font-normal text-text-muted"
        )}
      >
        {pending ? "Cargando…" : empty ? "sin datos" : children}
      </span>
      {detail && !empty && <span className="text-xs text-text-muted">{detail}</span>}
    </div>
  )
}

export { StatCard, StatValue }
