import { describe, expect, it } from 'vitest'
import { ApiError } from '@/lib/api'
import { mapChannelError, mapScopeError } from './errors'
import { describeScopeMismatch, GLOBAL_OWNER, ownerLabel, ownerToGroupId, ownerValue, scopeMismatchRules } from './owner'

describe('dueño del canal', () => {
  it('convierte entre selector y contrato', () => {
    expect(ownerValue({ group_id: null })).toBe(GLOBAL_OWNER)
    expect(ownerValue({ group_id: 'g1' })).toBe('g1')
    expect(ownerToGroupId(GLOBAL_OWNER)).toBeNull()
    expect(ownerToGroupId('g1')).toBe('g1')
  })
  it('etiqueta', () => {
    expect(ownerLabel({ group_id: null, group_name: null })).toBe('Global')
    expect(ownerLabel({ group_id: 'g1', group_name: 'Acme' })).toBe('Acme')
  })
  it('lee y describe las reglas del conflicto', () => {
    const details = { rules: [{ id: '1', name: 'CPU alta' }, { id: '2', name: 'Disco' }] }
    expect(scopeMismatchRules(details)).toEqual(['CPU alta', 'Disco'])
    expect(scopeMismatchRules(undefined)).toEqual([])
    expect(scopeMismatchRules({ rules: 'x' })).toEqual([])
    expect(describeScopeMismatch(details)).toContain('CPU alta, Disco')
    expect(describeScopeMismatch(undefined)).not.toContain('implicadas')
  })
  it('mapea errores de ámbito y grupo', () => {
    const e = new ApiError('channel_scope_mismatch', 'x', 422, undefined, { rules: [{ id: '1', name: 'R1' }] })
    expect(mapScopeError(e)).toContain('R1')
    expect(mapChannelError(e)).toContain('R1')
    expect(mapScopeError(new ApiError('group_not_found', 'x', 422))).toMatch(/grupo/)
    expect(mapScopeError(new ApiError('other', 'x', 400))).toBeNull()
  })
})
