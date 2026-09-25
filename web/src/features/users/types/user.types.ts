import type { Role } from '@/features/auth/types/auth.types'

/** Vista de administración de un usuario (GET /users). */
export interface ManagedUser {
  id: string
  username: string
  role: Role
  must_change_password: boolean
  disabled: boolean
  last_login_at: string | null
  created_at: string
  /** Permisos por grupo (solo restringibles en viewers; un admin siempre tiene all_groups). */
  all_groups: boolean
  group_ids: string[]
  can_inventory: boolean
}

export interface UserListResponse {
  items: ManagedUser[]
}

export interface CreateUserInput {
  username: string
  password: string
  role: Role
}

/** Cambio parcial: el servidor exige al menos un campo. */
export interface UpdateUserInput {
  role?: Role
  disabled?: boolean
  new_password?: string
  all_groups?: boolean
  /** Reemplaza la lista completa; solo con all_groups=false. */
  group_ids?: string[]
  can_inventory?: boolean
}
