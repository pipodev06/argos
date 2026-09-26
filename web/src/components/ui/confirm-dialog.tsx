import type { ReactNode } from "react"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { InlineError } from "@/components/ui/inline-error"

interface ConfirmDialogProps {
  open: boolean
  title: string
  description: ReactNode
  confirmLabel: string
  busy: boolean
  error: string | null
  /** Contenido extra bajo la descripcion (p. ej. un campo de confirmacion). */
  children?: ReactNode
  /** Deshabilita el boton de confirmar (p. ej. hasta teclear el nombre). */
  confirmDisabled?: boolean
  onConfirm: () => void
  onClose: () => void
}

// Confirmacion unica de acciones (eliminar, resolver...): la logica y el estado ocupado/error viven en quien la usa.
function ConfirmDialog({ open, title, description, confirmLabel, busy, error, children, confirmDisabled = false, onConfirm, onClose }: ConfirmDialogProps) {
  return (
    <AlertDialog open={open} onOpenChange={(next) => !next && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription>{description}</AlertDialogDescription>
        </AlertDialogHeader>
        {children}
        {error && <InlineError>{error}</InlineError>}
        <AlertDialogFooter>
          <AlertDialogCancel>Cancelar</AlertDialogCancel>
          <AlertDialogAction
            disabled={busy || confirmDisabled}
            onClick={(e) => {
              e.preventDefault()
              onConfirm()
            }}
          >
            {busy ? "Procesando..." : confirmLabel}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

export { ConfirmDialog }
