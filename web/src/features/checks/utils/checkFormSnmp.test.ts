import { describe, expect, it } from 'vitest'
import type { ServiceCheck } from '../types/check.types'
import { buildCheckInput, checkToDraft } from './checkForm'
import { checkScopeLabel, checkTypeLabel, detailLabel } from './format'

const base = { ...checkToDraft(null), name: 'SNMP core', type: 'snmp' as const }
const all = { type: 'all', group_id: null, group_name: null } as const

describe('chequeo snmp: formulario -> payload', () => {
  it('reachable: sin if_index, umbrales ni destino', () => {
    const r = buildCheckInput({ ...base, warnMs: '100', host: 'x' }, null)
    expect(r).toEqual({
      input: { name: 'SNMP core', enabled: true, interval_seconds: 60, timeout_ms: 5000, retries: 1, scope: { type: 'all' }, type: 'snmp', metric: 'reachable' },
    })
  })
  it('if_oper exige ifIndex entero 1-65535', () => {
    for (const bad of ['', '0', '65536', '3.5', 'a']) {
      const r = buildCheckInput({ ...base, metric: 'if_oper', ifIndex: bad }, null)
      expect('errors' in r && r.errors.ifIndex).toBeTruthy()
    }
    const ok = buildCheckInput({ ...base, metric: 'if_oper', ifIndex: '65535' }, null)
    expect('input' in ok && ok.input.if_index).toBe(65535)
  })
  it('alcance de un dispositivo vale para snmp', () => {
    const r = buildCheckInput({ ...base, scope: { type: 'device', groupId: '', deviceId: 'd1' } }, null)
    expect('input' in r && r.input.scope).toEqual({ type: 'device', device_id: 'd1' })
  })
  it('editar: pasar de if_oper a reachable quita if_index; el tipo no se envía', () => {
    const orig = { type: 'snmp', metric: 'if_oper', if_index: 3, scope: all } as unknown as ServiceCheck
    const r = buildCheckInput({ ...base, metric: 'reachable' }, orig)
    expect('input' in r && r.input).toMatchObject({ metric: 'reachable', if_index: null })
    if ('input' in r) expect(r.input).not.toHaveProperty('type')
  })
})

describe('chequeo snmp: textos', () => {
  it('tipo con métrica', () => {
    expect(checkTypeLabel({ type: 'snmp', metric: 'if_oper', if_index: 3 })).toBe('SNMP · Estado de interfaz #3')
    expect(checkTypeLabel({ type: 'snmp', metric: 'reachable', if_index: null })).toBe('SNMP · Responde (uptime)')
    expect(checkTypeLabel({ type: 'ping' })).toBe('Ping')
  })
  it('alcance todos = dispositivos y detalles nuevos', () => {
    expect(checkScopeLabel({ type: 'snmp', scope: all } as unknown as ServiceCheck)).toBe('Todos los dispositivos')
    expect(detailLabel('auth_failed')).toMatch(/SNMP/)
    expect(detailLabel('no_such_object')).toMatch(/interfaz/)
  })
})
