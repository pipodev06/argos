import { createContext, useContext } from 'react'
import type { AuthStatus, LoginCredentials, User } from '../types/auth.types'

export interface AuthContextValue {
  status: AuthStatus
  user: User | null
  login: (credentials: LoginCredentials) => Promise<void>
  /** Cierra la sesion en el servidor; si falla por red/servidor lanza y la sesion local se conserva. */
  logout: () => Promise<void>
  /** Vuelve a consultar GET /auth/me (p. ej. tras cambiar la clave, para bajar must_change_password). */
  refresh: () => Promise<void>
}

export const AuthContext = createContext<AuthContextValue | null>(null)

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth debe usarse dentro de <AuthProvider>')
  return ctx
}
