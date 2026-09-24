import { useCallback, useState } from 'react'
import { ApiError } from '@/lib/api'
import type { LoginCredentials } from '../types/auth.types'
import { formatRetryAfter } from '../utils/format'
import { useAuth } from './useAuth'

// Mensaje uniforme: no distingue usuario inexistente de clave incorrecta.
const INVALID_CREDENTIALS = 'Usuario o contraseña incorrectos.'

function loginErrorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.code === 'invalid_credentials') return INVALID_CREDENTIALS
    if (err.code === 'rate_limited') {
      return err.retryAfter === undefined
        ? 'Demasiados intentos. Espera un momento y reintenta.'
        : `Demasiados intentos. Reintenta en ${formatRetryAfter(err.retryAfter)}.`
    }
    return err.message
  }
  return 'Error desconocido'
}

export function useLogin() {
  const { login } = useAuth()
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  /** Devuelve true si el login fue exitoso (el guard cambia de pantalla solo). */
  const submit = useCallback(
    async (credentials: LoginCredentials) => {
      setSubmitting(true)
      setError(null)
      try {
        await login(credentials)
        return true
      } catch (err) {
        setError(loginErrorMessage(err))
        return false
      } finally {
        setSubmitting(false)
      }
    },
    [login],
  )

  return { error, submitting, submit }
}
