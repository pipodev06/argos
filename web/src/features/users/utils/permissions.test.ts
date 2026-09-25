import { describe, expect, it } from 'vitest'
import type { Group } from '@/features/groups/types/group.types'
import type { ManagedUser } from '../types/user.types'
import { accessLabel, filterGroups, formFromUser, isSameForm, MAX_USER_GROUPS, toggleGroupId, toPayload } from './permissions'

const base: ManagedUser = {
  id: 'u1', username: 'ana', role: 'viewer', must_change_password: false, disabled: false,
  last_login_at: null, created_at: '2026-01-01T00:00:00Z', all_groups: false, group_ids: ['a', 'b'], can_inventory: true, can_updates: true,
}

describe('permisos: formulario y payload', () => {
  it('mapea usuario a formulario sin compartir la lista', () => {
    const f = formFromUser(base)
    expect(f).toEqual({ allGroups: false, groupIds: ['a', 'b'], canInventory: true, canUpdates: true })
    expect(f.groupIds).not.toBe(base.group_ids)
  })
  it('all_groups=true no envía group_ids', () => {
    expect(toPayload({ allGroups: true, groupIds: ['a'], canInventory: false, canUpdates: true })).toEqual({ all_groups: true, can_inventory: false, can_updates: true })
  })
  it('restringido envía la lista', () => {
    expect(toPayload({ allGroups: false, groupIds: ['a'], canInventory: true, canUpdates: false })).toEqual({ all_groups: false, group_ids: ['a'], can_inventory: true, can_updates: false })
  })
  it('isSameForm ignora el orden y las listas con all_groups', () => {
    const a = { allGroups: false, groupIds: ['a', 'b'], canInventory: false, canUpdates: false }
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
