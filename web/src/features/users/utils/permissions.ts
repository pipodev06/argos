import type { Group } from '@/features/groups/types/group.types'
import type { Role } from '@/features/auth/types/auth.types'
import type { CreateUserInput, ManagedUser, UpdateUserInput } from '../types/user.types'

/** Tope de grupos por usuario (el servidor responde 400 sobre esto). */
export const MAX_USER_GROUPS = 200

/** Sobre este número de grupos el diálogo muestra buscador. */
export const GROUP_SEARCH_THRESHOLD = 8

export interface PermissionsForm {
  allGroups: boolean
  groupIds: string[]
  canInventory: boolean
  canUpdates: boolean
  canAck: boolean
}

export function formFromUser(u: ManagedUser): PermissionsForm {
  return { allGroups: u.all_groups, groupIds: [...u.group_ids], canInventory: u.can_inventory, canUpdates: u.can_updates, canAck: u.can_ack }
}

/** Con all_groups=true el servidor rechaza group_ids (invalid_scope): solo se envía al restringir. */
export function toPayload(f: PermissionsForm): UpdateUserInput {
  const perms = { can_inventory: f.canInventory, can_updates: f.canUpdates, can_ack: f.canAck }
  if (f.allGroups) return { all_groups: true, ...perms }
  return { all_groups: false, group_ids: [...f.groupIds], ...perms }
}

export function isSameForm(a: PermissionsForm, b: PermissionsForm): boolean {
  if (a.allGroups !== b.allGroups || a.canInventory !== b.canInventory || a.canUpdates !== b.canUpdates || a.canAck !== b.canAck) return false
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

export const EMPTY_FORM: PermissionsForm = { allGroups: false, groupIds: [], canInventory: false, canUpdates: false, canAck: false }

/** Alta: un admin no lleva grupos ni permisos; un viewer envía su ámbito (all_groups=true no envía group_ids). */
export function toCreatePayload(username: string, password: string, role: Role, f: PermissionsForm): CreateUserInput {
  if (role === 'admin') return { username, password, role }
  return { username, password, role, ...toPayload(f) }
}

/** Viewer sin grupos ni "todos": el alta se permite, pero no verá nada. */
export function seesNothing(role: Role, f: PermissionsForm): boolean {
  return role === 'viewer' && !f.allGroups && f.groupIds.length === 0
}

/** Mensaje de error del alta/borrado según el código del servidor (null = usar el del servidor). */
export function mapCreateUserError(code: string): string | null {
  if (code === 'group_not_found') return 'Alguno de los grupos ya no existe. Cierra y vuelve a abrir el formulario.'
  if (code === 'invalid_scope') return 'Esa combinación de acceso no es válida.'
  return null
}

export function mapDeleteUserError(code: string): string | null {
  if (code === 'self_delete') return 'No puedes eliminar tu propia cuenta.'
  if (code === 'last_admin') return 'Debe quedar al menos un administrador activo.'
  return null
}
