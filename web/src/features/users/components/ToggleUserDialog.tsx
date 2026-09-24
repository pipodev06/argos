import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { InlineError } from '@/components/ui/inline-error'
import { useUserMutation } from '../hooks/useUserMutation'
import { updateUser } from '../services/users.service'
import type { ManagedUser } from '../types/user.types'

interface ToggleUserDialogProps {
  user: ManagedUser | null
  onClose: () => void
  onSaved: () => void
}

/** Deshabilitar (revoca sus sesiones) o habilitar, según el estado actual del usuario. */
export function ToggleUserDialog({ user, onClose, onSaved }: ToggleUserDialogProps) {
  const { error, busy, run, clearError } = useUserMutation()
  const enabling = user?.disabled === true

  function handleOpenChange(next: boolean) {
    if (next) return
    clearError()
    onClose()
  }

  async function handleConfirm() {
    if (!user) return
    const ok = await run(
      () => updateUser(user.id, { disabled: !user.disabled }),
      (err) => (err.code === 'last_admin' ? 'Debe quedar al menos un administrador activo.' : null),
    )
    if (ok) onSaved()
  }

  return (
    <AlertDialog open={user !== null} onOpenChange={handleOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            {enabling ? 'Habilitar' : 'Deshabilitar'} a &quot;{user?.username ?? ''}&quot;
          </AlertDialogTitle>
          <AlertDialogDescription>
            {enabling
              ? 'Podrá volver a iniciar sesión con su contraseña actual.'
              : 'Se cerrarán todas sus sesiones y no podrá iniciar sesión hasta que lo habilites de nuevo.'}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {error && <InlineError>{error}</InlineError>}
        <AlertDialogFooter>
          <AlertDialogCancel>Cancelar</AlertDialogCancel>
          <AlertDialogAction
            disabled={busy}
            onClick={(e) => {
              e.preventDefault()
              void handleConfirm()
            }}
          >
            {busy ? 'Guardando...' : enabling ? 'Habilitar' : 'Deshabilitar'}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
