import { cva } from "class-variance-authority"

// brand solo en acciones: variante default (primaria) y anillo de foco. Deshabilitado = plano y muted (sin
// opacity: el primario al 50% quedaba ilegible en oscuro).
export const buttonVariants = cva(
  "inline-flex shrink-0 items-center justify-center rounded-md border border-transparent text-sm font-medium whitespace-nowrap transition-colors outline-none select-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-bg disabled:pointer-events-none [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground hover:bg-primary/90 disabled:bg-muted disabled:text-text-muted",
        destructive: "bg-danger text-bg hover:bg-danger/90 disabled:bg-muted disabled:text-text-muted",
        outline: "border-border bg-surface text-foreground hover:bg-muted disabled:text-text-muted",
        ghost: "text-foreground hover:bg-muted disabled:text-text-muted",
      },
      size: {
        default: "h-8 gap-1.5 px-2 py-1",
        sm: "h-7 gap-1 px-2 py-1",
        icon: "size-8",
        "icon-sm": "size-7",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  }
)
