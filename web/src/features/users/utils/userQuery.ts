import type { FilterField, FiltersOf } from '@/components/filters/types'
import type { ManagedUser } from '../types/user.types'

export const USER_FIELDS = [
  { kind: 'search', key: 'q', label: 'Buscar', ariaLabel: 'Buscar usuarios', placeholder: 'Buscar usuario', maxLength: 64 },
  {
    kind: 'multi',
    key: 'role',
    label: 'Rol',
    plural: 'roles',
    options: [
      { id: 'admin', name: 'Administrador' },
      { id: 'viewer', name: 'Solo lectura' },
    ],
  },
  {
    kind: 'multi',
    key: 'status',
    label: 'Estado',
    plural: 'estados',
    options: [
      { id: 'active', name: 'Activo' },
      { id: 'disabled', name: 'Deshabilitado' },
    ],
  },
] as const satisfies readonly FilterField[]

export type UserFilters = FiltersOf<typeof USER_FIELDS>

/** La API no filtra: se filtra en cliente (multi = OR dentro del campo, AND entre campos). */
export function filterUsers(users: readonly ManagedUser[], f: UserFilters): ManagedUser[] {
  const q = f.q.trim().toLowerCase()
  return users.filter(
    (u) =>
      (!q || u.username.toLowerCase().includes(q)) &&
      (f.role.length === 0 || f.role.includes(u.role)) &&
      (f.status.length === 0 || f.status.includes(u.disabled ? 'disabled' : 'active')),
  )
}

export const MAX_PASSWORD = 128

/** Largo en code points (igual que el servidor). */
export function passwordLength(value: string): number {
  return [...value].length
}

export function passwordError(value: string, min: number): string | null {
  const n = passwordLength(value)
  if (n < min) return `Mínimo ${min} caracteres.`
  if (n > MAX_PASSWORD) return `Máximo ${MAX_PASSWORD} caracteres.`
  return null
}
