import * as React from "react"
import { CircleAlertIcon } from "lucide-react"
import { cn } from "cn"

// Estado de error: el color danger va solo en el icono; el texto usa `text`.
function InlineError({ className, children, ...props }: React.ComponentProps<"p">) {
  return (
    <p role="alert" className={cn("flex items-start gap-1.5 text-sm text-text", className)} {...props}>
      <CircleAlertIcon className="mt-0.5 size-4 shrink-0 text-danger" strokeWidth={1.5} aria-hidden />
      <span>{children}</span>
    </p>
  )
}

export { InlineError }
