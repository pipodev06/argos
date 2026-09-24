import type { ReactNode } from 'react'
import { useAuth } from '../hooks/useAuth'
import { ForcedPasswordPage } from './ForcedPasswordPage'
import { LoginPage } from './LoginPage'

// Route guard: sin sesion -> login; con sesion -> children; must_change_password -> solo cambio de clave.
export function AuthGate({ children }: { children: ReactNode }) {
  const { status, user } = useAuth()

  // Hasta la primera respuesta de /auth/me solo se pinta el fondo: evita parpadeo del login.
  if (status === 'loading') {
    return (
      <div role="status" className="min-h-svh bg-bg">
        <span className="sr-only">Cargando…</span>
      </div>
    )
  }
  if (!user) return <LoginPage />
  if (user.must_change_password) return <ForcedPasswordPage />
  return children
}
