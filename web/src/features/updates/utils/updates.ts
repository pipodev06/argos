import type { User } from '@/features/auth/types/auth.types'
import { formatNumber } from '@/features/hosts/utils/format'
import type { Eol } from '../types/updates.types'

const DAY_MS = 86_400_000

/** Admin o permiso explicito (el servidor lo vuelve a validar y responde 404 si no). */
export function canViewUpdates(user: Pick<User, 'role' | 'can_updates'> | null | undefined): boolean {
  return !!user && (user.role === 'admin' || user.can_updates)
}

/** Conteo: `null` = desconocido (nunca se muestra como 0). */
export function countText(n: number | null): string {
  return n === null ? 'Desconocido' : formatNumber(n, 0)
}

export type RebootState = 'yes' | 'no' | 'unknown'

/** "Sí, desde hace N d" (si se conoce desde cuando), "No" o "Desconocido". */
export function rebootInfo(required: boolean | null, since: string | null, now: number = Date.now()): { state: RebootState; text: string } {
  if (required === null) return { state: 'unknown', text: 'Desconocido' }
  if (!required) return { state: 'no', text: 'No' }
  const t = since ? Date.parse(since) : NaN
  if (Number.isNaN(t)) return { state: 'yes', text: 'Sí' }
  const days = Math.max(0, Math.floor((now - t) / DAY_MS))
  return { state: 'yes', text: days === 0 ? 'Sí, desde hoy' : `Sí, desde hace ${days} d` }
}

export type EolState = 'none' | 'ok' | 'soon' | 'expired'

/** Umbral de "pronto" para el icono de fin de vida (dias). */
export const EOL_SOON_DAYS = 90

/** Fin de vida: fecha + "faltan N d" / "Vencido". Sin dato = "Desconocido". */
export function eolInfo(eol: Eol | null): { state: EolState; text: string } {
  if (!eol) return { state: 'none', text: 'Desconocido' }
  // Las fechas estimadas ya no se rotulan (decision del usuario 2026-09-25): el texto es el mismo que el de una oficial.
  if (eol.expired || eol.days_left <= 0) return { state: 'expired', text: `Vencido desde ${eol.eol_date}` }
  return {
    state: eol.days_left <= EOL_SOON_DAYS ? 'soon' : 'ok',
    text: `${eol.eol_date}, faltan ${eol.days_left} d`,
  }
}

/** Edad del dato en segundos: "hace 5 min", "hace 3 h", "hace 2 d". */
export function formatDataAge(seconds: number): string {
  if (seconds < 60) return 'hace menos de 1 min'
  if (seconds < 3600) return `hace ${Math.floor(seconds / 60)} min`
  if (seconds < 86_400) return `hace ${Math.floor(seconds / 3600)} h`
  return `hace ${Math.floor(seconds / 86_400)} d`
}

/** Ultima actualizacion instalada (YYYY-MM-DD); null = desconocido. */
export function lastInstalledText(d: string | null): string {
  return d ?? 'Desconocido'
}
