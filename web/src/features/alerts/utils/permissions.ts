import type { User } from '@/features/auth/types/auth.types'

type Actor = Pick<User, 'role' | 'can_ack'> | null | undefined

/** Reconocer: admin o viewer con el permiso `can_ack`. */
export function canAckAlerts(user: Actor): boolean {
  return !!user && (user.role === 'admin' || user.can_ack)
}

/** Resolver: solo admin. */
export function canResolveAlerts(user: Actor): boolean {
  return user?.role === 'admin'
}
