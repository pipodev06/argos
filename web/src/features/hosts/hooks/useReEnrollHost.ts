import { useCallback, useState } from 'react'
import { reEnrollHost } from '../services/hosts.service'
import type { EnrollToken } from '../types/host.types'

/** Re-enrola (o reactiva) un host existente: token nuevo via POST /hosts/{id}/re-enroll. */
export function useReEnrollHost(id: string) {
  const [token, setToken] = useState<EnrollToken | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  const submit = useCallback(async () => {
    setSubmitting(true)
    setError(null)
    try {
      setToken(await reEnrollHost(id))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Error desconocido')
    } finally {
      setSubmitting(false)
    }
  }, [id])

  // Borra el token del estado (al cerrar el dialogo o al generar otro).
  const reset = useCallback(() => {
    setToken(null)
    setError(null)
  }, [])

  return { token, error, submitting, submit, reset }
}
