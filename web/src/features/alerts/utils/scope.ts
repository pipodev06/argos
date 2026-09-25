import type { Scope, ScopeInput, ScopeType } from '../types/alert.types'

/** Alcance en edición (todo texto, como los campos del formulario). */
export interface ScopeDraft {
  type: ScopeType
  groupId: string
  /** Solo chequeos ping. */
  deviceId?: string
}

export const ALL_SCOPE: ScopeDraft = { type: 'all', groupId: '' }

export const SCOPE_TYPE_OPTIONS: readonly { value: ScopeType; label: string }[] = [
  { value: 'all', label: 'Todos los hosts' },
  { value: 'group', label: 'Un grupo' },
]

/** Alcance de un chequeo ping: sus objetivos son dispositivos, no hosts con agente. */
export const PING_SCOPE_TYPE_OPTIONS: readonly { value: ScopeType; label: string }[] = [
  { value: 'all', label: 'Todos los dispositivos' },
  { value: 'group', label: 'Un grupo' },
  { value: 'device', label: 'Un dispositivo' },
]

export function scopeToDraft(s: Scope): ScopeDraft {
  return { type: s.type, groupId: s.group_id ?? '', deviceId: s.device_id }
}

export function draftToScope(d: ScopeDraft): { scope: ScopeInput } | { error: string } {
  if (d.type === 'group') {
    return d.groupId ? { scope: { type: 'group', group_id: d.groupId } } : { error: 'Elige un grupo.' }
  }
  if (d.type === 'device') {
    return d.deviceId ? { scope: { type: 'device', device_id: d.deviceId } } : { error: 'Elige un dispositivo.' }
  }
  return { scope: { type: 'all' } }
}
