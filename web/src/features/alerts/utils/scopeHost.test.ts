import { describe, expect, it } from 'vitest'
import { draftToScope, ruleScopeTypes, scopeOptions, scopeToDraft } from './scope'

describe('alcance host de un chequeo', () => {
  it('draftToScope envia host_id y exige elegir un host', () => {
    expect(draftToScope({ type: 'host', groupId: '', hostId: 'h1' })).toEqual({ scope: { type: 'host', host_id: 'h1' } })
    expect(draftToScope({ type: 'host', groupId: '' })).toEqual({ error: 'Elige un host.' })
  })
  it('scopeToDraft precarga el host al editar', () => {
    const d = scopeToDraft({ type: 'host', group_id: null, group_name: null, host_id: 'h1', host_name: 'mi-pc' })
    expect(d).toMatchObject({ type: 'host', hostId: 'h1' })
  })
})

describe('alcances de reglas y mantenimiento', () => {
  it('solo host_status puede apuntar a un dispositivo', () => {
    expect(ruleScopeTypes('host_status')).toEqual(['all', 'group', 'host', 'device'])
    expect(ruleScopeTypes('metric')).toEqual(['all', 'group', 'host'])
    expect(ruleScopeTypes('exposed_port')).not.toContain('device')
  })
  it('scopeOptions da los textos comunes', () => {
    expect(scopeOptions(['host', 'device']).map((o) => o.label)).toEqual(['Un host', 'Un dispositivo'])
  })
  it('draftToScope acepta host y dispositivo', () => {
    expect(draftToScope({ type: 'device', groupId: '', deviceId: 'd1' })).toEqual({ scope: { type: 'device', device_id: 'd1' } })
    expect(draftToScope({ type: 'device', groupId: '' })).toEqual({ error: 'Elige un dispositivo.' })
  })
})
