import type { Group } from '@/features/groups/types/group.types'
import type { ManagedUser, UpdateUserInput } from '../types/user.types'

/** Tope de grupos por usuario (el servidor responde 400 sobre esto). */
export const MAX_USER_GROUPS = 200

/** Sobre este número de grupos el diálogo muestra buscador. */
export const GROUP_SEARCH_THRESHOLD = 8

export interface PermissionsForm {
  allGroups: boolean
  groupIds: string[]
  canInventory: boolean
  canUpdates: boolean
}

export function formFromUser(u: ManagedUser): PermissionsForm {
  return { allGroups: u.all_groups, groupIds: [...u.group_ids], canInventory: u.can_inventory, canUpdates: u.can_updates }
}

/** Con all_groups=true el servidor rechaza group_ids (invalid_scope): solo se envía al restringir. */
export function toPayload(f: PermissionsForm): UpdateUserInput {
  if (f.allGroups) return { all_groups: true, can_inventory: f.canInventory, can_updates: f.canUpdates }
  return { all_groups: false, group_ids: [...f.groupIds], can_inventory: f.canInventory, can_updates: f.canUpdates }
}

export function isSameForm(a: PermissionsForm, b: PermissionsForm): boolean {
  if (a.allGroups !== b.allGroups || a.canInventory !== b.canInventory || a.canUpdates !== b.canUpdates) return false
  if (a.allGroups) return true
  return a.groupIds.length === b.groupIds.length && a.groupIds.every((id) => b.groupIds.includes(id))
}

/** Marca/desmarca un grupo respetando el tope; devuelve la misma lista si no cabe. */
export function toggleGroupId(ids: string[], id: string, on: boolean): string[] {
  if (!on) return ids.filter((g) => g !== id)
  if (ids.includes(id) || ids.length >= MAX_USER_GROUPS) return ids
  return [...ids, id]
}

export function filterGroups(groups: Group[], query: string): Group[] {
  const q = query.trim().toLowerCase()
  return q ? groups.filter((g) => g.name.toLowerCase().includes(q)) : groups
}

/** Texto de la columna "Acceso". */
export function accessLabel(u: Pick<ManagedUser, 'role' | 'all_groups' | 'group_ids'>): string {
  if (u.role === 'admin' || u.all_groups) return 'Todos los grupos'
  const n = u.group_ids.length
  return n === 1 ? '1 grupo' : `${n} grupos`
}
