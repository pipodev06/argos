import type { HostStatus } from '../types/host.types'
import { formatElapsed } from './format'

export const HOST_STATUS_LABELS: Record<HostStatus, string> = {
  up: 'Activo',
  down: 'Caído',
  offline: 'Sin conexión',
  pending: 'Pendiente',
  disabled: 'Deshabilitado',
}

// "Sin reportar hace 5 min." (o solo "Sin reportar." si el host nunca reporto).
function silentFor(lastSeenAt: string | null, now: number): string {
  return lastSeenAt ? `Sin reportar hace ${formatElapsed(lastSeenAt, now)}.` : 'Sin reportar.'
}

/** Texto de detalle de cada estado (unica fuente: lo usan el detalle de host y donde haga falta explicar el estado). */
export function hostStatusDetail(
  status: HostStatus,
  lastSeenAt: string | null,
  now: number = Date.now(),
): string {
  switch (status) {
    case 'up':
      return 'Reportando con normalidad.'
    case 'down':
      return `${silentFor(lastSeenAt, now)} Se espera que este equipo esté siempre encendido.`
    case 'offline':
      return `${silentFor(lastSeenAt, now)} Puede estar apagado o fuera de la red; no se considera una falla.`
    case 'pending':
      return 'Esperando el primer reporte.'
    case 'disabled':
      return 'Token revocado; el agente ya no puede reportar.'
  }
}
