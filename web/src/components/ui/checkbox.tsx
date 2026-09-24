import * as React from "react"
import { cn } from "cn"
import { Checkbox as CheckboxPrimitive } from "radix-ui"
import { IconCheck } from "@tabler/icons-react"

// Slate: borde `text-muted` (>= 3:1), marcado = relleno neutro `text` + icono (no `brand`: no es una accion).
// `after:` amplia el area tactil a 32px sin cambiar el tamano visual.
function Checkbox({
  className,
  ...props
}: React.ComponentProps<typeof CheckboxPrimitive.Root>) {
  return (
    <CheckboxPrimitive.Root
      data-slot="checkbox"
      className={cn(
        "peer relative flex size-4 shrink-0 items-center justify-center rounded-md border border-text-muted bg-surface text-bg transition-colors outline-none after:absolute after:-inset-2 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-bg disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:border-danger data-[state=checked]:border-text data-[state=checked]:bg-text data-[state=indeterminate]:border-text data-[state=indeterminate]:bg-text",
        className
      )}
      {...props}
    >
      <CheckboxPrimitive.Indicator
        data-slot="checkbox-indicator"
        className="grid place-content-center text-current"
      >
        <IconCheck className="size-3" stroke={2.5} aria-hidden />
      </CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  )
}

export { Checkbox }
