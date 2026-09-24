import { useCallback, useState } from 'react'
import { useAuth } from './useAuth'

// Logout con error visible: si el servidor no confirma, la sesion local se conserva.
export function useLogout() {
  const { logout } = useAuth()
  const [error, setError] = useState<string | null>(null)

  const submit = useCallback(async () => {
    setError(null)
    try {
      await logout()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Error desconocido')
    }
  }, [logout])

  return { error, logout: submit }
}
