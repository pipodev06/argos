import { useCallback, useState } from 'react'
import { createEnrollToken, type CreateEnrollTokenInput } from '../services/hosts.service'
import type { EnrollToken } from '../types/host.types'

export function useEnrollHost() {
  const [token, setToken] = useState<EnrollToken | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  const submit = useCallback(async (input: CreateEnrollTokenInput) => {
    setSubmitting(true)
    setError(null)
    try {
      setToken(await createEnrollToken(input))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Error desconocido')
    } finally {
      setSubmitting(false)
    }
  }, [])

  // Borra el token del estado (al cerrar el dialogo o al generar otro).
  const reset = useCallback(() => {
    setToken(null)
    setError(null)
  }, [])

  return { token, error, submitting, submit, reset }
}
