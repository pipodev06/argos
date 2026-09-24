import { useState, type FormEvent, type ReactNode } from 'react'
import { Button } from '@/components/ui/button'
import { FormField } from '@/components/ui/form-field'
import { InlineError } from '@/components/ui/inline-error'
import { MIN_PASSWORD_LENGTH } from '../constants'
import { useChangePassword } from '../hooks/useChangePassword'

interface ChangePasswordFormProps {
  onSuccess?: () => void
  /** Botones extra a la izquierda del submit (cancelar / cerrar sesion). */
  actions?: ReactNode
}

export function ChangePasswordForm({ onSuccess, actions }: ChangePasswordFormProps) {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [confirm, setConfirm] = useState('')
  const { errors, submitting, submit } = useChangePassword()

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    if (await submit({ current, next, confirm })) onSuccess?.()
  }

  return (
    <form onSubmit={(e) => void handleSubmit(e)} className="grid gap-4">
      <FormField
        id="current-password"
        label="Contraseña actual"
        type="password"
        value={current}
        onChange={(e) => setCurrent(e.target.value)}
        autoComplete="current-password"
        error={errors.current}
        autoFocus
      />
      <FormField
        id="new-password"
        label="Contraseña nueva"
        type="password"
        value={next}
        onChange={(e) => setNext(e.target.value)}
        autoComplete="new-password"
        hint={`Mínimo ${MIN_PASSWORD_LENGTH} caracteres. Puedes usar una frase.`}
        error={errors.next}
      />
      <FormField
        id="confirm-password"
        label="Repite la contraseña nueva"
        type="password"
        value={confirm}
        onChange={(e) => setConfirm(e.target.value)}
        autoComplete="new-password"
        error={errors.confirm}
      />
      {errors.form && <InlineError>{errors.form}</InlineError>}
      <div className="flex justify-end gap-2">
        {actions}
        <Button type="submit" disabled={submitting}>
          {submitting ? 'Guardando...' : 'Cambiar contraseña'}
        </Button>
      </div>
    </form>
  )
}
