import { describe, expect, it } from 'vitest'
import { draftToScope, scopeToDraft } from './scope'

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
