import type { ReactNode } from "react"
import type { Icon } from "@tabler/icons-react"

interface EmptyStateProps {
  Icon?: Icon
  title: string
  description?: ReactNode
  /** Accion sugerida (boton o enlace). */
  action?: ReactNode
}

/** Estado vacio unico: panel con borde, icono opcional, titulo (`sm` semibold), ayuda y accion. */
function EmptyState({ Icon, title, description, action }: EmptyStateProps) {
  return (
    <div className="flex flex-col items-center gap-2 rounded-md border border-border bg-surface px-6 py-10 text-center">
      {Icon && <Icon className="size-6 text-text-muted" stroke={1.5} aria-hidden />}
      <p className="text-sm font-semibold">{title}</p>
      {description && <p className="max-w-md text-sm text-text-muted">{description}</p>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  )
}

export { EmptyState }
