import { describe, expect, it } from 'vitest'
import { formatAgentVersion } from './agentVersion'
import { buildApiQuery, HOST_FIELDS } from './hostQuery'
import { parseFilters } from '@/components/filters/urlState'

describe('versión del agente', () => {
  it('formato: dev y vacío como raya', () => {
    expect(formatAgentVersion('1.2.3')).toBe('1.2.3')
    expect(formatAgentVersion('dev')).toBe('—')
    expect(formatAgentVersion('')).toBe('—')
    expect(formatAgentVersion(null)).toBe('—')
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
