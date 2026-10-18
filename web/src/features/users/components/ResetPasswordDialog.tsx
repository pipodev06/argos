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
import { MIN_PASSWORD_LENGTH } from '@/features/auth/constants'
import { useUserMutation } from '../hooks/useUserMutation'
import { updateUser } from '../services/users.service'
import type { ManagedUser } from '../types/user.types'
import { passwordError } from '../utils/userQuery'
import { PasswordField } from './PasswordField'

interface ResetPasswordDialogProps {
  user: ManagedUser | null
  onClose: () => void
  onSaved: () => void
}

export function ResetPasswordDialog(props: ResetPasswordDialogProps) {
  // El formulario se monta solo abierto: al cerrar se descarta la contraseña.
  return (
    <Dialog open={props.user !== null} onOpenChange={(next) => !next && props.onClose()}>
      <DialogContent>{props.user && <ResetForm {...props} user={props.user} />}</DialogContent>
    </Dialog>
  )
}

function ResetForm({ user, onClose, onSaved }: ResetPasswordDialogProps & { user: ManagedUser }) {
  const [password, setPassword] = useState('')
  const [passError, setPassError] = useState<string | null>(null)
  const { error, busy, run } = useUserMutation()

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setPassError(null)
    const pe = passwordError(password, MIN_PASSWORD_LENGTH)
    if (pe) return setPassError(pe)
    const ok = await run(
      () => updateUser(user.id, { new_password: password }),
      (err) => {
        if (err.code !== 'weak_password') return null
        setPassError(err.message)
        return ''
      },
    )
    if (ok) onSaved()
  }

  return (
    <form onSubmit={(e) => void handleSubmit(e)} className="grid gap-3">
      <DialogHeader>
        <DialogTitle>Restablecer contraseña de {user.username}</DialogTitle>
      </DialogHeader>
      <PasswordField id="reset-password" label="Contraseña temporal" value={password} onChange={setPassword} error={passError} />
      {error && <InlineError>{error}</InlineError>}
      <DialogFooter>
        <Button type="button" variant="ghost" onClick={onClose}>
          Cancelar
        </Button>
        <Button type="submit" disabled={busy}>
          {busy ? 'Guardando...' : 'Restablecer'}
        </Button>
      </DialogFooter>
    </form>
  )
}
