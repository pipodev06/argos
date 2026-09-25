import type { Scope, ScopeInput, ScopeType } from '../types/alert.types'

/** Alcance en edición (todo texto, como los campos del formulario). */
export interface ScopeDraft {
  type: ScopeType
  groupId: string
  /** Solo chequeos ping. */
  deviceId?: string
  /** Solo chequeos de servicio (no ping/snmp). */
  hostId?: string
}

export const ALL_SCOPE: ScopeDraft = { type: 'all', groupId: '' }

export const SCOPE_TYPE_OPTIONS: readonly { value: ScopeType; label: string }[] = [
  { value: 'all', label: 'Todos los hosts' },
  { value: 'group', label: 'Un grupo' },
]

/** Alcance de un chequeo de servicio (tcp, http, tls, servicio): puede apuntar a un host concreto con agente. */
export const CHECK_SCOPE_TYPE_OPTIONS: readonly { value: ScopeType; label: string }[] = [
  { value: 'all', label: 'Todos los hosts' },
  { value: 'group', label: 'Un grupo' },
  { value: 'host', label: 'Un host' },
]

/** Alcance de un chequeo ping: sus objetivos son dispositivos, no hosts con agente. */
export const PING_SCOPE_TYPE_OPTIONS: readonly { value: ScopeType; label: string }[] = [
  { value: 'all', label: 'Todos los dispositivos' },
  { value: 'group', label: 'Un grupo' },
  { value: 'device', label: 'Un dispositivo' },
]

const SCOPE_LABELS: Record<ScopeType, string> = {
  all: 'Todos los hosts',
  group: 'Un grupo',
  host: 'Un host',
  device: 'Un dispositivo',
}

/** Opciones de alcance para una lista de tipos permitidos (mismos textos en reglas y mantenimiento). */
export function scopeOptions(types: readonly ScopeType[]): readonly { value: ScopeType; label: string }[] {
  return types.map((value) => ({ value, label: SCOPE_LABELS[value] }))
}

/** Alcances permitidos para una regla segun su tipo: solo host_status puede apuntar a un dispositivo; el resto necesita agente. */
export function ruleScopeTypes(kind: string): readonly ScopeType[] {
  return kind === 'host_status' ? ['all', 'group', 'host', 'device'] : ['all', 'group', 'host']
}

/** Ventana de mantenimiento: cualquier destino. */
export const MAINTENANCE_SCOPE_TYPES: readonly ScopeType[] = ['all', 'group', 'host', 'device']

export function scopeToDraft(s: Scope): ScopeDraft {
  return { type: s.type, groupId: s.group_id ?? '', deviceId: s.device_id, hostId: s.host_id }
}

export function draftToScope(d: ScopeDraft): { scope: ScopeInput } | { error: string } {
  if (d.type === 'group') {
    return d.groupId ? { scope: { type: 'group', group_id: d.groupId } } : { error: 'Elige un grupo.' }
  }
  if (d.type === 'device') {
    return d.deviceId ? { scope: { type: 'device', device_id: d.deviceId } } : { error: 'Elige un dispositivo.' }
  }
  if (d.type === 'host') {
    return d.hostId ? { scope: { type: 'host', host_id: d.hostId } } : { error: 'Elige un host.' }
  }
  return { scope: { type: 'all' } }
}
