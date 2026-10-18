import type { ReactNode } from "react"

interface PageHeaderProps {
  title: string
  /** Ayuda junto al titulo (p. ej. `FormHelp`). */
  help?: ReactNode
  /** Acciones de la pagina (botones), alineadas a la derecha. */
  actions?: ReactNode
}

/** Encabezado unico de pagina: titulo (`lg`) a la izquierda y acciones a la derecha. */
function PageHeader({ title, help, actions }: PageHeaderProps) {
  return (
    <div className="mb-3 flex min-h-8 flex-wrap items-center justify-between gap-3">
      <div className="flex items-center gap-1">
        <h1 className="text-lg font-semibold">{title}</h1>
        {help}
      </div>
      {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
    </div>
  )
}

export { PageHeader }
