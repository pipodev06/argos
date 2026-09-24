import { useState } from 'react'
import { ApiError } from '@/lib/api'

/** Estado común de los diálogos de usuario: ocupado + mensaje de error (el del servidor si lo hay). */
export function useUserMutation() {
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  /** Devuelve true si salió bien. `map` traduce códigos de error conocidos (null = usar el mensaje del servidor). */
  async function run(op: () => Promise<unknown>, map?: (err: ApiError) => string | null): Promise<boolean> {
    setBusy(true)
    setError(null)
    try {
      await op()
      return true
    } catch (err) {
      const mapped = err instanceof ApiError ? (map?.(err) ?? null) : null
      setError(mapped ?? (err instanceof Error ? err.message : 'Error desconocido'))
      return false
    } finally {
      setBusy(false)
    }
  }

  return { error, busy, run, clearError: () => setError(null) }
}
