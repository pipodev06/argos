import { describe, expect, it } from 'vitest'
import { parseFilters } from '@/components/filters/urlState'
import type { Eol } from '../types/updates.types'
import { buildFleetQuery, FLEET_FIELDS } from './fleetQuery'
import { canViewUpdates, countText, eolInfo, formatDataAge, rebootInfo } from './updates'

const NOW = Date.parse('2026-09-25T12:00:00Z')
const eol = (o: Partial<Eol>): Eol => ({ label: 'Ubuntu 20.04', eol_date: '2026-12-01', days_left: 67, expired: false, estimated: false, lts: true, ...o })

describe('parches: textos', () => {
  it('null es Desconocido, nunca 0', () => {
    expect(countText(null)).toBe('Desconocido')
    expect(countText(0)).toBe('0')
    expect(rebootInfo(null, null, NOW)).toEqual({ state: 'unknown', text: 'Desconocido' })
  })
  it('reinicio pendiente con y sin fecha de inicio', () => {
    expect(rebootInfo(false, null, NOW)).toEqual({ state: 'no', text: 'No' })
    expect(rebootInfo(true, '2026-09-11T12:00:00Z', NOW).text).toBe('Sí, desde hace 14 d')
    expect(rebootInfo(true, '2026-09-25T08:00:00Z', NOW).text).toBe('Sí, desde hoy')
    expect(rebootInfo(true, null, NOW).text).toBe('Sí')
  })
  it('fin de vida: faltan N d, pronto, vencido, estimado y sin dato', () => {
    expect(eolInfo(eol({ days_left: 200 }))).toEqual({ state: 'ok', text: '2026-12-01, faltan 200 d' })
    expect(eolInfo(eol({})).state).toBe('soon')
    expect(eolInfo(eol({ days_left: 30 })).state).toBe('soon')
    expect(eolInfo(eol({ expired: true, days_left: -10, eol_date: '2026-09-15' }))).toEqual({ state: 'expired', text: 'Vencido desde 2026-09-15' })
    expect(eolInfo(eol({ estimated: true })).text).toBe('2026-12-01, faltan 67 d (estimado)')
    expect(eolInfo(null).state).toBe('none')
  })
  it('edad del dato', () => {
    expect(formatDataAge(20)).toBe('hace menos de 1 min')
    expect(formatDataAge(300)).toBe('hace 5 min')
    expect(formatDataAge(7200)).toBe('hace 2 h')
    expect(formatDataAge(172_800)).toBe('hace 2 d')
  })
  it('visibilidad: admin o can_updates', () => {
    expect(canViewUpdates({ role: 'admin', can_updates: false })).toBe(true)
    expect(canViewUpdates({ role: 'viewer', can_updates: true })).toBe(true)
    expect(canViewUpdates({ role: 'viewer', can_updates: false })).toBe(false)
    expect(canViewUpdates(null)).toBe(false)
  })
})

describe('parches: query de la flota', () => {
  it('valores por defecto explícitos, sin flags', () => {
    const f = parseFilters(FLEET_FIELDS, new URLSearchParams())
    expect(buildFleetQuery(f, null).toString()).toBe('sort=security&dir=desc&limit=15')
  })
  it('flags -> security/reboot, eol_days, grupos y cursor', () => {
    const f = parseFilters(FLEET_FIELDS, new URLSearchParams('flags=security&flags=reboot&eol_days=90&group=none&os=linux&sort=eol&dir=asc&limit=50&q=web'))
    const p = buildFleetQuery(f, 'abc')
    expect(p.get('security')).toBe('true')
    expect(p.get('reboot')).toBe('true')
    expect(p.has('flags')).toBe(false)
    expect(p.get('eol_days')).toBe('90')
    expect(p.getAll('group')).toEqual(['none'])
    expect(p.get('sort')).toBe('eol')
    expect(p.get('dir')).toBe('asc')
    expect(p.get('limit')).toBe('50')
    expect(p.get('cursor')).toBe('abc')
  })
})
