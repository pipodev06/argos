import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { isUnauthenticated, onUnauthorized } from '@/lib/api'
import { AuthContext, type AuthContextValue } from '../hooks/useAuth'
import * as authService from '../services/auth.service'
import type { AuthStatus, LoginCredentials, User } from '../types/auth.types'

interface SessionState {
  status: AuthStatus
  user: User | null
}

const LOGGED_OUT: SessionState = { status: 'unauthenticated', user: null }

function authenticated(user: User): SessionState {
  return { status: 'authenticated', user }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<SessionState>({ status: 'loading', user: null })

  useEffect(() => {
    const controller = new AbortController()
    // 401 en cualquier llamada (sesion expirada) -> limpia la sesion y el guard vuelve al login.
    const stopListening = onUnauthorized(() => setSession(LOGGED_OUT))
    authService
      .fetchMe(controller.signal)
      .then((user) => setSession(authenticated(user)))
      .catch(() => {
        if (!controller.signal.aborted) setSession(LOGGED_OUT)
      })
    return () => {
      controller.abort()
      stopListening()
    }
  }, [])

  const login = useCallback(async (credentials: LoginCredentials) => {
    setSession(authenticated(await authService.login(credentials)))
  }, [])

  const logout = useCallback(async () => {
    try {
      await authService.logout()
    } catch (err) {
      // 401 = ya no habia sesion (el listener central ya la limpio); cualquier otro fallo se propaga.
      if (!isUnauthenticated(err)) throw err
    }
    setSession(LOGGED_OUT)
  }, [])

  const refresh = useCallback(async () => {
    setSession(authenticated(await authService.fetchMe()))
  }, [])

  const value = useMemo<AuthContextValue>(
    () => ({ ...session, login, logout, refresh }),
    [session, login, logout, refresh],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}
