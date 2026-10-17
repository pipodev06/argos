import { useState } from 'react'
import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import { FormField } from '@/components/ui/form-field'
import { purgeNameMatches } from '@/features/purge/utils/purge'
import { useUserMutation } from '../hooks/useUserMutation'
import { deleteUser } from '../services/users.service'
import type { ManagedUser } from '../types/user.types'
import { mapDeleteUserError } from '../utils/permissions'

interface DeleteUserDialogProps {
  user: ManagedUser | null
  onClose: () => void
  onSaved: () => void
}

/** Eliminar un usuario: irreversible; exige teclear su nombre. */
export function DeleteUserDialog({ user, onClose, onSaved }: DeleteUserDialogProps) {
  // El campo vive en un hijo que se monta abierto: cada apertura empieza vacía.
  return user ? <DeleteUserForm user={user} onClose={onClose} onSaved={onSaved} /> : null
}

function DeleteUserForm({ user, onClose, onSaved }: DeleteUserDialogProps & { user: ManagedUser }) {
  const [typed, setTyped] = useState('')
  const { error, busy, run } = useUserMutation()

  async function confirm() {
    const ok = await run(
      () => deleteUser(user.id),
      (err) => mapDeleteUserError(err.code),
    )
    if (ok) onSaved()
  }

  return (
    <ConfirmDialog
      open
      title={`Eliminar a "${user.username}"`}
      description="Se elimina de forma definitiva el usuario, se cierran sus sesiones y se quitan sus permisos. La auditoría se conserva. Esta acción no se puede deshacer."
      confirmLabel="Eliminar usuario"
      destructive
      busy={busy}
      error={error}
      confirmDisabled={!purgeNameMatches(typed, user.username)}
      onConfirm={() => void confirm()}
      onClose={onClose}
    >
      <FormField
        id="delete-user-name"
        label={`Escribe "${user.username}" para confirmar`}
        value={typed}
        onChange={(e) => setTyped(e.target.value)}
        autoComplete="off"
        autoFocus
      />
    </ConfirmDialog>
  )
}
