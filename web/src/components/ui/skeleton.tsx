import * as React from "react"
import { cn } from "cn"

// Placeholder de carga: bloque `bg-border` que pulsa; sin animacion si el usuario pide menos movimiento.
function Skeleton({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="skeleton"
      aria-hidden
      className={cn("animate-pulse rounded-md bg-border motion-reduce:animate-none", className)}
      {...props}
    />
  )
}

export { Skeleton }
