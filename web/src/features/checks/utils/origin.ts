import type { Device } from '@/features/devices/types/device.types'
import type { ServiceCheck } from '../types/check.types'

// Los chequeos ping y snmp los ejecuta el COLECTOR de cada dispositivo; el resto, cada agente sobre si mismo.
const COLLECTOR_TYPES: readonly string[] = ['ping', 'snmp']
const MAX_NAMES = 2

/**
 * Origen de un chequeo: que equipo envia el dato. Ping/snmp: los colectores de los dispositivos del alcance
 * (el colector solo lo conoce un admin: un viewer ve "—"); demas tipos: los propios hosts del alcance.
 */
export function checkOrigin(check: Pick<ServiceCheck, 'type' | 'scope'>, devices: readonly Device[] | undefined): string {
  if (!COLLECTOR_TYPES.includes(check.type)) {
    return check.scope.type === 'group' ? 'Hosts del grupo' : 'Cada host con agente'
  }
  if (!devices) return '—'
  const inScope = devices.filter((d) => {
    if (check.scope.type === 'device') return d.id === check.scope.device_id
    if (check.scope.type === 'group') return d.group?.id === check.scope.group_id
    return true
  })
  if (inScope.length === 0) return 'Sin dispositivos'
  const names = [...new Set(inScope.flatMap((d) => (d.collector ? [d.collector.name] : [])))].sort((a, b) => a.localeCompare(b, 'es'))
  if (names.length === 0) return '—'
  const shown = names.slice(0, MAX_NAMES).join(', ')
  return names.length > MAX_NAMES ? `${shown} +${names.length - MAX_NAMES}` : shown
}
