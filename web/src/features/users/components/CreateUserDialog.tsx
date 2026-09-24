import { useState, type FormEvent } from 'react'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { FormField } from '@/components/ui/form-field'
import { InlineError } from '@/components/ui/inline-error'
import { MIN_PASSWORD_LENGTH } from '@/features/auth/constants'
import type { Role } from '@/features/auth/types/auth.types'
import { useUserMutation } from '../hooks/useUserMutation'
import { createUser } from '../services/users.service'
import { passwordError } from '../utils/userQuery'
import { PasswordField } from './PasswordField'
import { RoleSelect } from './RoleSelect'

interface CreateUserDialogProps {
  open: boolean
  onClose: () => void
  onSaved: () => void
}

export function CreateUserDialog({ open, onClose, onSaved }: CreateUserDialogProps) {
  // El formulario se monta solo abierto: al cerrar se descartan usuario y contraseña.
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent>{open && <CreateUserForm onClose={onClose} onSaved={onSaved} />}</DialogContent>
    </Dialog>
  )
}

function CreateUserForm({ onClose, onSaved }: Omit<CreateUserDialogProps, 'open'>) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [role, setRole] = useState<Role>('viewer')
  const [userError, setUserError] = useState<string | null>(null)
  const [passError, setPassError] = useState<string | null>(null)
  const { error, busy, run } = useUserMutation()

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    const name = username.trim()
    setUserError(null)
    setPassError(null)
    if (name.length < 3 || name.length > 64) return setUserError('Entre 3 y 64 caracteres.')
    const pe = passwordError(password, MIN_PASSWORD_LENGTH)
    if (pe) return setPassError(pe)
    const ok = await run(
      () => createUser({ username: name, password, role }),
      (err) => {
        if (err.code === 'weak_password') setPassError(err.message)
        else if (err.status === 409) setUserError('Ya existe un usuario con ese nombre.')
        else return null
        return ''
      },
    )
    if (ok) onSaved()
  }

  return (
    <form onSubmit={(e) => void handleSubmit(e)} className="grid gap-4">
      <DialogHeader>
        <DialogTitle>Nuevo usuario</DialogTitle>
        <DialogDescription>
          La contraseña es temporal: el usuario deberá cambiarla al entrar por primera vez.
        </DialogDescription>
      </DialogHeader>
      <FormField
        id="user-name"
        label="Usuario"
        value={username}
        onChange={(e) => setUsername(e.target.value)}
        maxLength={64}
        autoComplete="off"
        autoFocus
        hint="Letras, números y . _ @ + -"
        error={userError}
      />
      <PasswordField id="user-password" label="Contraseña inicial" value={password} onChange={setPassword} error={passError} />
      <RoleSelect id="user-role" label="Rol" value={role} onChange={setRole} />
      {error && <InlineError>{error}</InlineError>}
      <DialogFooter>
        <Button type="button" variant="ghost" onClick={onClose}>
          Cancelar
        </Button>
        <Button type="submit" disabled={busy}>
          {busy ? 'Creando...' : 'Crear usuario'}
        </Button>
      </DialogFooter>
    </form>
  )
}
