import type { Scope, ScopeInput, ScopeType } from '../types/alert.types'

/** Alcance en edición (todo texto, como los campos del formulario). */
export interface ScopeDraft {
  type: ScopeType
  groupId: string
}

export const ALL_SCOPE: ScopeDraft = { type: 'all', groupId: '' }

export const SCOPE_TYPE_OPTIONS: readonly { value: ScopeType; label: string }[] = [
  { value: 'all', label: 'Todos los hosts' },
  { value: 'group', label: 'Un grupo' },
]

export function scopeToDraft(s: Scope): ScopeDraft {
  return { type: s.type, groupId: s.group_id ?? '' }
}

export function draftToScope(d: ScopeDraft): { scope: ScopeInput } | { error: string } {
  if (d.type === 'group') {
    return d.groupId ? { scope: { type: 'group', group_id: d.groupId } } : { error: 'Elige un grupo.' }
  }
  return { scope: { type: 'all' } }
}
