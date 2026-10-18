import { useState, type FormEvent } from 'react'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { FormHelp } from '@/components/ui/form-help'
import { PERMISSIONS_HELP } from '../utils/help'
import { InlineError } from '@/components/ui/inline-error'
import { useUserMutation } from '../hooks/useUserMutation'
import { updateUser } from '../services/users.service'
import type { ManagedUser } from '../types/user.types'
import { formFromUser, isSameForm, toPayload } from '../utils/permissions'
import { PermissionsFields } from './PermissionsFields'

interface PermissionsDialogProps {
  user: ManagedUser | null
  onClose: () => void
  onSaved: () => void
}

export function PermissionsDialog(props: PermissionsDialogProps) {
  return (
    <Dialog open={props.user !== null} onOpenChange={(next) => !next && props.onClose()}>
      <DialogContent>{props.user && <PermissionsForm {...props} user={props.user} />}</DialogContent>
    </Dialog>
  )
}

function PermissionsForm({ user, onClose, onSaved }: PermissionsDialogProps & { user: ManagedUser }) {
  const [initial] = useState(() => formFromUser(user))
  const [form, setForm] = useState(initial)
  const { error, busy, run } = useUserMutation()

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    if (isSameForm(form, initial)) return onClose()
    const ok = await run(
      () => updateUser(user.id, toPayload(form)),
      (err) => (err.code === 'invalid_scope' ? 'Esa combinación de permisos no es válida para este usuario.' : null),
    )
    if (ok) onSaved()
  }

  return (
    <form onSubmit={(e) => void handleSubmit(e)} className="grid gap-3">
      <DialogHeader className="flex-row items-center gap-1">
        <DialogTitle>Permisos de {user.username}</DialogTitle>
        <FormHelp title="Permisos" items={PERMISSIONS_HELP} />
      </DialogHeader>
      <PermissionsFields idPrefix="perm" form={form} onChange={setForm} />
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
