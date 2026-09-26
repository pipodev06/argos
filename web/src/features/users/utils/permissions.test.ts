import { describe, expect, it } from 'vitest'
import type { Group } from '@/features/groups/types/group.types'
import type { ManagedUser } from '../types/user.types'
import {
  accessLabel, EMPTY_FORM, filterGroups, formFromUser, isSameForm, mapCreateUserError, mapDeleteUserError, MAX_USER_GROUPS, seesNothing, toCreatePayload, toggleGroupId, toPayload,
} from './permissions'

const base: ManagedUser = {
  id: 'u1', username: 'ana', role: 'viewer', must_change_password: false, disabled: false,
  last_login_at: null, created_at: '2026-01-01T00:00:00Z', all_groups: false, group_ids: ['a', 'b'], can_inventory: true, can_updates: true, can_ack: false,
}

describe('permisos: formulario y payload', () => {
  it('mapea usuario a formulario sin compartir la lista', () => {
    const f = formFromUser(base)
    expect(f).toEqual({ allGroups: false, groupIds: ['a', 'b'], canInventory: true, canUpdates: true, canAck: false })
    expect(f.groupIds).not.toBe(base.group_ids)
  })
  it('all_groups=true no envía group_ids', () => {
    expect(toPayload({ allGroups: true, groupIds: ['a'], canInventory: false, canUpdates: true, canAck: true })).toEqual({ all_groups: true, can_inventory: false, can_updates: true, can_ack: true })
  })
  it('restringido envía la lista', () => {
    expect(toPayload({ allGroups: false, groupIds: ['a'], canInventory: true, canUpdates: false, canAck: false })).toEqual({ all_groups: false, group_ids: ['a'], can_inventory: true, can_updates: false, can_ack: false })
  })
  it('isSameForm ignora el orden y las listas con all_groups', () => {
    const a = { allGroups: false, groupIds: ['a', 'b'], canInventory: false, canUpdates: false, canAck: false }
    expect(isSameForm(a, { ...a, groupIds: ['b', 'a'] })).toBe(true)
    expect(isSameForm(a, { ...a, canUpdates: true })).toBe(false)
    expect(isSameForm(a, { ...a, groupIds: ['a'] })).toBe(false)
    expect(isSameForm({ ...a, allGroups: true }, { ...a, allGroups: true, groupIds: [] })).toBe(true)
  })
  it('toggleGroupId agrega, quita y respeta el tope', () => {
    expect(toggleGroupId(['a'], 'b', true)).toEqual(['a', 'b'])
    expect(toggleGroupId(['a', 'b'], 'a', false)).toEqual(['b'])
    expect(toggleGroupId(['a'], 'a', true)).toEqual(['a'])
    const full = Array.from({ length: MAX_USER_GROUPS }, (_, i) => `g${i}`)
    expect(toggleGroupId(full, 'x', true)).toBe(full)
  })
  it('filterGroups busca sin distinguir mayúsculas', () => {
    const gs = [{ id: '1', name: 'Producción' }, { id: '2', name: 'QA' }] as Group[]
    expect(filterGroups(gs, ' prod ').map((g) => g.id)).toEqual(['1'])
    expect(filterGroups(gs, '')).toBe(gs)
  })
  it('accessLabel', () => {
    expect(accessLabel({ ...base, all_groups: true })).toBe('Todos los grupos')
    expect(accessLabel({ ...base, role: 'admin', all_groups: false })).toBe('Todos los grupos')
    expect(accessLabel({ ...base, group_ids: ['a'] })).toBe('1 grupo')
    expect(accessLabel(base)).toBe('2 grupos')
  })
})

describe('alta de usuario', () => {
  const f = { allGroups: false, groupIds: ['a'], canInventory: true, canUpdates: false, canAck: true }
  it('admin no envía ámbito ni permisos', () => {
    expect(toCreatePayload('root', 'x', 'admin', f)).toEqual({ username: 'root', password: 'x', role: 'admin' })
  })
  it('viewer con grupos', () => {
    expect(toCreatePayload('ana', 'x', 'viewer', f)).toEqual({
      username: 'ana', password: 'x', role: 'viewer', all_groups: false, group_ids: ['a'], can_inventory: true, can_updates: false, can_ack: true,
    })
  })
  it('viewer con todos no envía group_ids', () => {
    const p = toCreatePayload('ana', 'x', 'viewer', { ...f, allGroups: true })
    expect(p.all_groups).toBe(true)
    expect(p).not.toHaveProperty('group_ids')
  })
  it('seesNothing solo para viewer sin ámbito', () => {
    expect(seesNothing('viewer', EMPTY_FORM)).toBe(true)
    expect(seesNothing('viewer', f)).toBe(false)
    expect(seesNothing('viewer', { ...EMPTY_FORM, allGroups: true })).toBe(false)
    expect(seesNothing('admin', EMPTY_FORM)).toBe(false)
  })
  it('mapea errores', () => {
    expect(mapCreateUserError('group_not_found')).toMatch(/grupos/)
    expect(mapCreateUserError('x')).toBeNull()
    expect(mapDeleteUserError('self_delete')).toMatch(/propia/)
    expect(mapDeleteUserError('last_admin')).toMatch(/administrador/)
    expect(mapDeleteUserError('other')).toBeNull()
  })
})
