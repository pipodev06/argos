import { describe, expect, it } from 'vitest'
import { canViewInventory, describeChange, describeValue, emptyKind, isExposed } from './inventory'

describe('isExposed', () => {
  it.each(['127.0.0.1', '127.1.2.3', '::1', '[::1]', '::ffff:127.0.0.1'])('%s es solo local', (b) => {
    expect(isExposed(b)).toBe(false)
  })
  it.each(['0.0.0.0', '::', '192.168.1.5', '::ffff:10.0.0.1', '', '1127.0.0.1'])('"%s" es expuesto', (b) => {
    expect(isExposed(b)).toBe(true)
  })
})

describe('emptyKind', () => {
  it('distingue desactivado, sin datos y vacio', () => {
    expect(emptyKind('disabled', 0)).toBe('disabled')
    expect(emptyKind('no_data', 0)).toBe('no_data')
    expect(emptyKind('ok', 0)).toBe('empty')
    expect(emptyKind('ok', 3)).toBeNull()
  })
})

describe('describeChange', () => {
  it('overflow usa dropped', () => {
    expect(describeChange('overflow', null, { dropped: 12 })).toBe('Se omitieron 12 cambios')
  })
  it('cambio de servicio legible', () => {
    expect(describeChange('changed', { state: 'running', start: 'auto' }, { state: 'stopped', start: 'auto' })).toBe(
      'estado: en ejecución, inicio: automático → estado: detenido, inicio: automático',
    )
  })
  it('added/removed usan el lado que existe', () => {
    expect(describeChange('added', null, { proc: 'nginx', owner_known: false })).toBe('proceso: nginx, dueño conocido: no')
    expect(describeChange('removed', { proc: 'x' }, null)).toBe('proceso: x')
  })
  it('null es guion', () => {
    expect(describeValue(null)).toBe('—')
  })
})

describe('canViewInventory', () => {
  it('admin o permiso', () => {
    expect(canViewInventory({ role: 'admin', can_inventory: false })).toBe(true)
    expect(canViewInventory({ role: 'viewer', can_inventory: true })).toBe(true)
    expect(canViewInventory({ role: 'viewer', can_inventory: false })).toBe(false)
    expect(canViewInventory(null)).toBe(false)
  })
})
