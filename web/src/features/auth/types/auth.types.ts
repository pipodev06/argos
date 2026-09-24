export type Role = 'admin' | 'viewer'

export interface User {
  id: string
  username: string
  role: Role
  must_change_password: boolean
}

export interface LoginCredentials {
  username: string
  password: string
}

export interface ChangePasswordPayload {
  current_password: string
  new_password: string
}

// Estado de sesion: `loading` solo existe hasta la primera respuesta de GET /auth/me.
export type AuthStatus = 'loading' | 'authenticated' | 'unauthenticated'
