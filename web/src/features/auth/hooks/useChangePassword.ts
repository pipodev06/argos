import { useCallback, useState } from 'react'
import { ApiError } from '@/lib/api'
import { MIN_PASSWORD_LENGTH } from '../constants'
import * as authService from '../services/auth.service'
import { useAuth } from './useAuth'

export interface ChangePasswordValues {
  current: string
  next: string
  confirm: string
}

export interface ChangePasswordErrors {
  current?: string
  next?: string
  confirm?: string
  form?: string
}

function validate({ current, next, confirm }: ChangePasswordValues): ChangePasswordErrors {
  const errors: ChangePasswordErrors = {}
  if (!current) errors.current = 'Ingresa tu contraseña actual.'
  // Cuenta caracteres (code points), no unidades UTF-16.
  if ([...next].length < MIN_PASSWORD_LENGTH) {
    errors.next = `Debe tener al menos ${MIN_PASSWORD_LENGTH} caracteres.`
  }
  if (confirm !== next) errors.confirm = 'Las contraseñas no coinciden.'
  return errors
}

export function useChangePassword() {
  const { refresh } = useAuth()
  const [errors, setErrors] = useState<ChangePasswordErrors>({})
  const [submitting, setSubmitting] = useState(false)

  /** Devuelve true si la clave se cambio. */
  const submit = useCallback(
    async (values: ChangePasswordValues) => {
      const invalid = validate(values)
      setErrors(invalid)
      if (Object.keys(invalid).length > 0) return false

      setSubmitting(true)
      try {
        await authService.changePassword({
          current_password: values.current,
          new_password: values.next,
        })
      } catch (err) {
        if (err instanceof ApiError && err.code === 'invalid_credentials') {
          setErrors({ current: 'La contraseña actual no es correcta.' })
        } else if (err instanceof ApiError && err.code === 'weak_password') {
          setErrors({ next: err.message })
        } else {
          setErrors({ form: err instanceof Error ? err.message : 'Error desconocido' })
        }
        setSubmitting(false)
        return false
      }
      try {
        // Baja must_change_password: si estaba forzado, el guard pasa solo al dashboard.
        await refresh()
      } catch (err) {
        setErrors({
          form: `La contraseña se cambió, pero no se pudo actualizar la sesión: ${
            err instanceof Error ? err.message : 'Error desconocido'
          }`,
        })
        setSubmitting(false)
        return false
      }
      setSubmitting(false)
      return true
    },
    [refresh],
  )

  const clearErrors = useCallback(() => setErrors({}), [])

  return { errors, submitting, submit, clearErrors }
}
