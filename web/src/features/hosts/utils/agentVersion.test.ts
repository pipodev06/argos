import { describe, expect, it } from 'vitest'
import { formatAgentVersion, isAgentOutdated, outdatedAgentCount } from './agentVersion'
import { buildApiQuery, HOST_FIELDS } from './hostQuery'
import { parseFilters } from '@/components/filters/urlState'

describe('versión del agente', () => {
  it('formato: dev y vacío como raya', () => {
    expect(formatAgentVersion('1.2.3')).toBe('1.2.3')
    expect(formatAgentVersion('dev')).toBe('—')
    expect(formatAgentVersion('')).toBe('—')
    expect(formatAgentVersion(null)).toBe('—')
  })
  it('desactualizado según las reglas del servidor', () => {
    expect(isAgentOutdated('1.2.0', '1.3.0')).toBe(true)
    expect(isAgentOutdated('1.3.0', '1.3.0')).toBe(false)
    expect(isAgentOutdated('1.4.0', '1.3.0')).toBe(false)
    expect(isAgentOutdated('v1.2', '1.10.0')).toBe(true)
    expect(isAgentOutdated('1.3.0-rc1', '1.3.0')).toBe(true)
    expect(isAgentOutdated('dev', '1.3.0')).toBe(false)
    expect(isAgentOutdated('', '1.3.0')).toBe(false)
    expect(isAgentOutdated('raro', '1.3.0')).toBe(true)
  })
  it('cuenta hosts desactualizados', () => {
    const counts = [{ version: '1.0.0', hosts: 3 }, { version: '1.3.0', hosts: 5 }, { version: 'dev', hosts: 1 }]
    expect(outdatedAgentCount({ latest: '1.3.0', counts })).toBe(3)
    expect(outdatedAgentCount({ latest: null, counts })).toBe(0)
  })
})

describe('filtro agent de hosts', () => {
  it('el chip vive en la URL como agent_outdated y la API recibe agent=outdated', () => {
    const f = parseFilters(HOST_FIELDS, new URLSearchParams('agent_outdated=true'))
    expect(f.agent_outdated).toBe(true)
    const q = buildApiQuery(f, null)
    expect(q.get('agent')).toBe('outdated')
    expect(q.has('agent_outdated')).toBe(false)
    expect(buildApiQuery(parseFilters(HOST_FIELDS, new URLSearchParams('')), null).has('agent')).toBe(false)
  })
})
