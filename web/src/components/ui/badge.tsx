import * as React from "react"
import { cn } from "cn"
import type { Icon as TablerIcon } from "@tabler/icons-react"

/** Tinte de estado: pinta el fondo (10%) y el icono; el texto va siempre en `text` (regla de color de AGENTS.md). */
export type BadgeTint = "ok" | "warn" | "danger" | "info" | "muted"

const TINTS: Record<BadgeTint, { bg: string; icon: string }> = {
  ok: { bg: "bg-ok/10", icon: "text-ok" },
  warn: { bg: "bg-warn/10", icon: "text-warn" },
  danger: { bg: "bg-danger/10", icon: "text-danger" },
  info: { bg: "bg-info/10", icon: "text-info" },
  muted: { bg: "bg-text-muted/10", icon: "text-text-muted" },
}

/**
 * Pill unica de la app (shadcn badge adaptado a Slate: radio 6px, tinte en vez de relleno): icono + texto, nunca
 * solo color. Todos los badges de estado (host, chequeo, alerta, auditoria...) se arman con este.
 */
function Badge({
  tint = "muted",
  Icon,
  className,
  children,
  ...props
}: React.ComponentProps<"span"> & { tint?: BadgeTint; Icon?: TablerIcon }) {
  const { bg, icon } = TINTS[tint]
  return (
    <span
      data-slot="badge"
      className={cn(
        "inline-flex w-fit shrink-0 items-center gap-1 rounded-md px-2 py-0.5 text-xs font-medium whitespace-nowrap text-text",
        bg,
        className
      )}
      {...props}
    >
      {Icon && <Icon className={cn("size-3.5 shrink-0", icon)} stroke={1.5} aria-hidden />}
      {children}
    </span>
  )
}

export { Badge }
