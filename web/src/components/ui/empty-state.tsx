import type { ReactNode } from "react"
import type { LucideIcon as Icon } from "lucide-react"

interface EmptyStateProps {
  Icon?: Icon
  title: string
  /** Accion sugerida (boton o enlace). */
  action?: ReactNode
}

/** Estado vacio unico: panel con borde, icono opcional, una sola linea (`sm` muted) y accion opcional. Sin textos de ayuda. */
function EmptyState({ Icon, title, action }: EmptyStateProps) {
  return (
    <div className="flex flex-col items-center gap-2 rounded-md border border-border bg-surface px-6 py-10 text-center">
      {Icon && <Icon className="size-6 text-text-muted" strokeWidth={1.5} aria-hidden />}
      <p className="text-sm text-text-muted">{title}</p>
      {action && <div className="mt-2">{action}</div>}
    </div>
  )
}

export { EmptyState }
