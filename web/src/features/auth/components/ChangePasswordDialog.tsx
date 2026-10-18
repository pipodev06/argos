import { useState } from 'react'
import { CircleCheckIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { ChangePasswordForm } from './ChangePasswordForm'

interface ChangePasswordDialogProps {
  open: boolean
  onClose: () => void
}

export function ChangePasswordDialog({ open, onClose }: ChangePasswordDialogProps) {
  const [done, setDone] = useState(false)

  function handleOpenChange(next: boolean) {
    if (next) return
    setDone(false)
    onClose()
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Cambiar contraseña</DialogTitle>
          {done && <DialogDescription>Tu contraseña se actualizó.</DialogDescription>}
        </DialogHeader>
        {done ? (
          <>
            <p className="flex items-center gap-1.5 text-sm">
              <CircleCheckIcon className="size-4 shrink-0 text-ok" strokeWidth={1.5} aria-hidden />
              Contraseña actualizada
            </p>
            <DialogFooter>
              <Button type="button" onClick={() => handleOpenChange(false)}>
                Cerrar
              </Button>
            </DialogFooter>
          </>
        ) : (
          <ChangePasswordForm
            onSuccess={() => setDone(true)}
            actions={
              <Button type="button" variant="ghost" onClick={() => handleOpenChange(false)}>
                Cancelar
              </Button>
            }
          />
        )}
      </DialogContent>
    </Dialog>
  )
}
