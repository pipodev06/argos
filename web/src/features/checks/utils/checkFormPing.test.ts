import { describe, expect, it } from 'vitest'
import type { ServiceCheck } from '../types/check.types'
import { buildCheckInput, checkToDraft } from './checkForm'
import { checkScopeLabel, checkTarget } from './format'

const base = { ...checkToDraft(null), name: 'Ping red', type: 'ping' as const }

describe('chequeo ping: formulario -> payload', () => {
  it('sin destino, con umbrales y alcance de todos', () => {
    const r = buildCheckInput({ ...base, warnMs: '100', critMs: '500' }, null)
    expect(r).toEqual({
      input: { name: 'Ping red', enabled: true, interval_seconds: 60, timeout_ms: 5000, retries: 1, scope: { type: 'all' }, type: 'ping', warn_ms: 100, crit_ms: 500 },
    })
    if ('input' in r) expect(r.input).not.toHaveProperty('target_host')
  })
  it('alcance de un dispositivo', () => {
    const r = buildCheckInput({ ...base, scope: { type: 'device', groupId: '', deviceId: 'dev-1' } }, null)
    expect('input' in r && r.input.scope).toEqual({ type: 'device', device_id: 'dev-1' })
  })
  it('dispositivo sin elegir o en un tipo que no es ping: error de alcance', () => {
    const none = buildCheckInput({ ...base, scope: { type: 'device', groupId: '' } }, null)
    expect('errors' in none && none.errors.scope).toBeTruthy()
    const tcp = buildCheckInput({ ...base, type: 'tcp', host: 'a.b', port: '80', scope: { type: 'device', groupId: '', deviceId: 'x' } }, null)
    expect('errors' in tcp && tcp.errors.scope).toBeTruthy()
  })
  it('warn >= crit se rechaza', () => {
    const r = buildCheckInput({ ...base, warnMs: '500', critMs: '500' }, null)
    expect('errors' in r && r.errors.warnMs).toBeTruthy()
  })
})

describe('chequeo ping: textos de la tabla', () => {
  const ping = (scope: ServiceCheck['scope']) => ({ type: 'ping', scope, target_host: null, service_name: null }) as unknown as ServiceCheck
  it('alcance', () => {
    expect(checkScopeLabel(ping({ type: 'all', group_id: null, group_name: null }))).toBe('Todos los dispositivos')
    expect(checkScopeLabel(ping({ type: 'device', group_id: null, group_name: null, device_id: 'd', device_name: 'Switch' }))).toBe('Dispositivo: Switch')
    expect(checkScopeLabel({ type: 'tcp', scope: { type: 'all', group_id: null, group_name: null } } as unknown as ServiceCheck)).toBe('Todos los hosts')
  })
  it('destino', () => {
    expect(checkTarget(ping({ type: 'device', group_id: null, group_name: null, device_id: 'd', device_name: 'Switch' }))).toBe('Switch')
    expect(checkTarget(ping({ type: 'all', group_id: null, group_name: null }))).toBe('Dirección del dispositivo')
  })
})
