import type { ApiError } from '@/lib/api'
import { mapCollectorRuleError } from './collectorRule'
import type { Device, DeviceInput } from '../types/device.types'

export const MAX_NAME = 100

const OCTET = '(?:25[0-5]|2[0-4]\\d|1\\d\\d|[1-9]?\\d)'
const IPV4_RE = new RegExp(`^${OCTET}(?:\\.${OCTET}){3}$`)
const HEX_GROUP_RE = /^[0-9A-Fa-f]{1,4}$/

/** IPv6 literal sin zona (`fe80::1%eth0` no vale): 8 grupos hex, una sola compresion `::` y cola IPv4 opcional. */
function isIPv6(raw: string): boolean {
  if (!raw.includes(':')) return false
  const halves = raw.split('::')
  if (halves.length > 2) return false
  const compressed = halves.length === 2
  let groups = 0
  const parts = compressed ? halves : [raw]
  for (let p = 0; p < parts.length; p++) {
    if (parts[p] === '') continue
    const items = parts[p].split(':')
    for (let i = 0; i < items.length; i++) {
      const item = items[i]
      const isLast = p === parts.length - 1 && i === items.length - 1
      if (isLast && item.includes('.')) {
        if (!IPV4_RE.test(item)) return false
        groups += 2
      } else if (HEX_GROUP_RE.test(item)) groups += 1
      else return false
    }
  }
  return compressed ? groups <= 7 : groups === 8
}

/** Forma de una IP literal (IPv4 o IPv6). El servidor decide el resto (loopback, link-local, etc.). */
export function isIpAddress(raw: string): boolean {
  const v = raw.trim()
  return IPV4_RE.test(v) || isIPv6(v)
}

export interface DeviceDraft {
  name: string
  address: string
  collectorId: string
  /** '' = sin grupo. */
  groupId: string
  /** '' = sin perfil SNMP. */
  snmpProfileId: string
}

export type DeviceErrors = Partial<Record<keyof DeviceDraft, string>>

export function deviceToDraft(d: Device | null): DeviceDraft {
  if (!d) return { name: '', address: '', collectorId: '', groupId: '', snmpProfileId: '' }
  return { name: d.name, address: d.address ?? '', collectorId: d.collector?.id ?? '', groupId: d.group?.id ?? '', snmpProfileId: d.snmp_profile?.id ?? '' }
}

/** Valida el borrador y arma el cuerpo. Editar (`original`): solo envia lo que cambio (el servidor exige al menos un campo). */
export function buildDeviceInput(d: DeviceDraft, original: Device | null): { input: DeviceInput } | { errors: DeviceErrors } {
  const errors: DeviceErrors = {}
  const name = d.name.trim()
  const address = d.address.trim()
  if (!name) errors.name = 'Ingresa un nombre.'
  else if (name.length > MAX_NAME) errors.name = `Máximo ${MAX_NAME} caracteres.`
  if (!address) errors.address = 'Ingresa la dirección IP.'
  else if (!isIpAddress(address)) errors.address = 'Ingresa una IP válida (IPv4 o IPv6), no un nombre.'
  if (!d.collectorId) errors.collectorId = 'Elige el colector que hará el ping.'
  if (Object.keys(errors).length > 0) return { errors }

  if (!original) {
    const input: DeviceInput = { name, address, collector_id: d.collectorId }
    if (d.groupId) input.group_id = d.groupId
    if (d.snmpProfileId) input.snmp_profile_id = d.snmpProfileId
    return { input }
  }
  const input: DeviceInput = {}
  if (name !== original.name) input.name = name
  if (address !== (original.address ?? '')) input.address = address
  if (d.collectorId !== (original.collector?.id ?? '')) input.collector_id = d.collectorId
  if (d.groupId !== (original.group?.id ?? '')) input.group_id = d.groupId || null
  if (d.snmpProfileId !== (original.snmp_profile?.id ?? '')) input.snmp_profile_id = d.snmpProfileId || null
  if (Object.keys(input).length === 0) return { errors: { name: 'No hay cambios que guardar.' } }
  return { input }
}

/** Traduce los errores conocidos de la API de dispositivos; null = usar el mensaje del servidor. */
export function mapDeviceError(err: ApiError): string | null {
  switch (err.code) {
    case 'device_exists':
      return 'Ya existe un dispositivo con ese nombre o esa dirección.'
    case 'too_many_devices':
      return 'Se alcanzó el máximo de 1000 dispositivos.'
    case 'collector_invalid':
      return 'El colector no es válido: debe ser un host con agente que no esté deshabilitado.'
    case 'group_not_found':
      return 'El grupo elegido ya no existe.'
    case 'profile_not_found':
      return 'El perfil SNMP elegido ya no existe.'
    case 'device_not_found':
      return 'El dispositivo ya no existe.'
    default:
      return mapCollectorRuleError(err)
  }
}
