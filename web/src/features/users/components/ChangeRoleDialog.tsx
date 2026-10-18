import { useState, type FormEvent } from 'react'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { InlineError } from '@/components/ui/inline-error'
import type { Role } from '@/features/auth/types/auth.types'
import { useUserMutation } from '../hooks/useUserMutation'
import { updateUser } from '../services/users.service'
import type { ManagedUser } from '../types/user.types'
import { RoleSelect } from './RoleSelect'

interface ChangeRoleDialogProps {
  user: ManagedUser | null
  onClose: () => void
  onSaved: () => void
}

export function ChangeRoleDialog(props: ChangeRoleDialogProps) {
  return (
    <Dialog open={props.user !== null} onOpenChange={(next) => !next && props.onClose()}>
      <DialogContent>{props.user && <RoleForm {...props} user={props.user} />}</DialogContent>
    </Dialog>
  )
}

function RoleForm({ user, onClose, onSaved }: ChangeRoleDialogProps & { user: ManagedUser }) {
  const [role, setRole] = useState<Role>(user.role)
  const { error, busy, run } = useUserMutation()

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    if (role === user.role) return onClose()
    const ok = await run(
      () => updateUser(user.id, { role }),
      (err) => (err.code === 'last_admin' ? 'Debe quedar al menos un administrador activo.' : null),
    )
    if (ok) onSaved()
  }

  return (
    <form onSubmit={(e) => void handleSubmit(e)} className="grid gap-3">
      <DialogHeader>
        <DialogTitle>Cambiar rol de {user.username}</DialogTitle>
      </DialogHeader>
      <RoleSelect id="change-role" label="Rol" value={role} onChange={setRole} />
      {error && <InlineError>{error}</InlineError>}
      <DialogFooter>
        <Button type="button" variant="ghost" onClick={onClose}>
          Cancelar
        </Button>
        <Button type="submit" disabled={busy}>
          {busy ? 'Guardando...' : 'Guardar'}
        </Button>
      </DialogFooter>
    </form>
  )
}
