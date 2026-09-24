import type { Role } from '../types/auth.types'

const ROLE_LABELS: Record<Role, string> = {
  admin: 'Administrador',
  viewer: 'Solo lectura',
}

export function roleLabel(role: Role): string {
  return ROLE_LABELS[role]
}

export function formatRetryAfter(seconds: number): string {
  if (seconds < 60) return `${seconds} s`
  return `${Math.ceil(seconds / 60)} min`
}
