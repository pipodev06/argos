import { describe, expect, it } from 'vitest'
import { canViewInventory, describeChange, describeValue, emptyKind, inventoryPath, isExposed, summaryValue } from './inventory'

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

describe('summaryValue / inventoryPath', () => {
  const now = Date.parse('2026-09-25T12:00:00Z')
  const base = { truncated: false, collected_at: null, count: 0 }
  it('ok: conteo y hace X', () => {
    expect(summaryValue({ ...base, status: 'ok', count: 42, updated_at: '2026-09-25T11:58:00Z' }, now)).toEqual({ text: 42, detail: 'hace 2 min' })
  })
  it('ok con 0 muestra 0; disabled y no_data', () => {
    expect(summaryValue({ ...base, status: 'ok', updated_at: null }, now)).toEqual({ text: 0, detail: undefined })
    expect(summaryValue({ ...base, status: 'disabled', updated_at: null }, now).text).toBe('Desactivado')
    expect(summaryValue({ ...base, status: 'no_data', updated_at: null }, now).text).toBeNull()
    expect(summaryValue(undefined, now).text).toBeNull()
  })
  it('ruta del host', () => {
    expect(inventoryPath('abc')).toBe('/hosts/abc/inventario')
  })
})
