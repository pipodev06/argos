import { describe, expect, it } from 'vitest'
import { parseFilters } from '@/components/filters/urlState'
import type { Eol } from '../types/updates.types'
import { buildFleetQuery, FLEET_FIELDS } from './fleetQuery'
import { buildPatchRows, describePatchRows } from './patchChart'
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
    expect(eolInfo(eol({ estimated: true })).text).toBe('2026-12-01, faltan 67 d')
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
  it('chips security/reboot, eol_days (≤), grupos y cursor', () => {
    const f = parseFilters(FLEET_FIELDS, new URLSearchParams('security=true&reboot=true&eol_days=90&group=none&os=linux&sort=eol&dir=asc&limit=50&q=web'))
    const p = buildFleetQuery(f, 'abc')
    expect(p.get('security')).toBe('true')
    expect(p.get('reboot')).toBe('true')
    expect(p.get('eol_days')).toBe('90')
    expect(p.getAll('group')).toEqual(['none'])
    expect(p.get('sort')).toBe('eol')
    expect(p.get('dir')).toBe('asc')
    expect(p.get('limit')).toBe('50')
    expect(p.get('cursor')).toBe('abc')
  })
  it('cada chip booleano va por separado y solo si está encendido', () => {
    const only = (qs: string) => buildFleetQuery(parseFilters(FLEET_FIELDS, new URLSearchParams(qs)), null)
    expect(only('security=true').get('reboot')).toBeNull()
    expect(only('security=true').get('security')).toBe('true')
    expect(only('reboot=true').get('security')).toBeNull()
    expect(only('security=false&reboot=1').has('security')).toBe(false)
    expect(only('reboot=1').has('reboot')).toBe(false)
  })
  it('eol_days usa ≤ en el chip y no acepta negativos', () => {
    const f = FLEET_FIELDS.find((x) => x.key === 'eol_days')
    expect(f?.kind === 'numeric' && f.op).toBe('≤')
    expect(parseFilters(FLEET_FIELDS, new URLSearchParams('eol_days=-3')).eol_days).toBeNull()
  })
})

describe('parches: gráfica del historial', () => {
  const pt = (at: string, total: number | null, security: number | null, reboot: boolean | null) => ({ at, pending_total: total, pending_security: security, reboot_required: reboot })
  it('ordena ascendente (el servidor manda lo más nuevo primero), conserva null y marca reinicio', () => {
    const rows = buildPatchRows([
      pt('2026-09-25T12:00:00Z', 12, 3, true),
      pt('2026-09-24T12:00:00Z', null, null, null),
      pt('2026-09-23T12:00:00Z', 20, 5, false),
    ])
    expect(rows.map((r) => r.total)).toEqual([20, null, 12])
    expect(rows.map((r) => r.security)).toEqual([5, null, 3])
    expect(rows.map((r) => r.reboot)).toEqual([false, false, true])
    expect(rows[0].t).toBe(Date.parse('2026-09-23T12:00:00Z'))
  })
  it('descarta fechas inválidas y vacío da lista vacía', () => {
    expect(buildPatchRows([pt('no-fecha', 1, 1, false)])).toEqual([])
    expect(buildPatchRows([])).toEqual([])
  })
  it('descripción accesible', () => {
    expect(describePatchRows([])).toBe('Sin datos de parches en el historial.')
    const d = describePatchRows(buildPatchRows([pt('2026-09-25T12:00:00Z', 12, 3, true), pt('2026-09-24T12:00:00Z', 20, 5, false)]))
    expect(d).toContain('2 puntos')
    expect(d).toContain('Último: 12 pendientes, 3 de seguridad')
    expect(d).toContain('Reinicio pendiente en 1 puntos')
  })
})
