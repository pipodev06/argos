import { describe, expect, it } from 'vitest'
import type { MaintenanceWindow } from '../types/alert.types'
import { buildMaintenanceInput, maintenanceToDraft, toLocalInput } from './maintenanceForm'

// 2026-09-26 12:00 hora local (las fechas del formulario son "datetime-local": sin zona).
const NOW = new Date(2026, 8, 26, 12, 0, 0).getTime()
const H = 3_600_000
const at = (offsetMs: number) => toLocalInput(new Date(NOW + offsetMs))
const draft = (over: Partial<ReturnType<typeof maintenanceToDraft>> = {}) => ({ ...maintenanceToDraft(null, NOW), name: 'Parche', ...over })
const errorsOf = (r: ReturnType<typeof buildMaintenanceInput>) => ('errors' in r ? r.errors : {})

describe('mantenimiento: borrador por defecto', () => {
  it('empieza ahora y dura 1 hora, sobre todos los hosts', () => {
    const d = maintenanceToDraft(null, NOW)
    expect(d.startsAt).toBe(at(0))
    expect(d.endsAt).toBe(at(H))
    expect(d.scope.type).toBe('all')
  })
})

describe('mantenimiento: validación', () => {
  it('nombre obligatorio y fechas obligatorias', () => {
    expect(errorsOf(buildMaintenanceInput(draft({ name: ' ' }), null, NOW))).toHaveProperty('name')
    expect(errorsOf(buildMaintenanceInput(draft({ startsAt: '' }), null, NOW))).toHaveProperty('startsAt')
    expect(errorsOf(buildMaintenanceInput(draft({ endsAt: '' }), null, NOW))).toHaveProperty('endsAt')
  })
  it('el fin debe ser posterior al inicio', () => {
    expect(errorsOf(buildMaintenanceInput(draft({ startsAt: at(H), endsAt: at(H) }), null, NOW))).toHaveProperty('endsAt')
    expect(errorsOf(buildMaintenanceInput(draft({ startsAt: at(2 * H), endsAt: at(H) }), null, NOW))).toHaveProperty('endsAt')
  })
  it('sobre todos los hosts: máximo 24 h; con un grupo, hasta 31 días', () => {
    expect(errorsOf(buildMaintenanceInput(draft({ endsAt: at(25 * H) }), null, NOW))).toHaveProperty('endsAt')
    expect(buildMaintenanceInput(draft({ endsAt: at(24 * H) }), null, NOW)).toHaveProperty('input')
    const group = { type: 'group' as const, groupId: 'g1' }
    expect(buildMaintenanceInput(draft({ scope: group, endsAt: at(31 * 24 * H) }), null, NOW)).toHaveProperty('input')
    expect(errorsOf(buildMaintenanceInput(draft({ scope: group, endsAt: at(31 * 24 * H + 60_000) }), null, NOW))).toHaveProperty('endsAt')
  })
  it('al crear: el fin no puede haber pasado ni el inicio ser de hace más de 1 h', () => {
    expect(errorsOf(buildMaintenanceInput(draft({ startsAt: at(-3 * H), endsAt: at(-2 * H) }), null, NOW))).toHaveProperty('endsAt')
    expect(errorsOf(buildMaintenanceInput(draft({ startsAt: at(-2 * H), endsAt: at(H) }), null, NOW))).toHaveProperty('startsAt')
    expect(buildMaintenanceInput(draft({ startsAt: at(-30 * 60_000), endsAt: at(H) }), null, NOW)).toHaveProperty('input')
  })
  it('alcance de host sin elegir: error de alcance', () => {
    expect(errorsOf(buildMaintenanceInput(draft({ scope: { type: 'host', groupId: '' } }), null, NOW))).toHaveProperty('scope')
  })
})

describe('mantenimiento: payload', () => {
  it('al crear envía nombre, alcance y fechas en ISO (UTC)', () => {
    const r = buildMaintenanceInput(draft(), null, NOW)
    expect(r).toMatchObject({ input: { name: 'Parche', scope: { type: 'all' } } })
    if ('input' in r) {
      expect(r.input.starts_at).toBe(new Date(NOW).toISOString())
      expect(r.input.ends_at).toBe(new Date(NOW + H).toISOString())
    }
  })
  it('al editar solo envía las fechas si cambiaron (una ventana en curso puede renombrarse)', () => {
    const w = {
      id: 'w1', name: 'En curso', starts_at: new Date(NOW - 2 * H).toISOString(), ends_at: new Date(NOW + H).toISOString(), scope: { type: 'all' },
    } as unknown as MaintenanceWindow
    const original = maintenanceToDraft(w, NOW)
    const renamed = buildMaintenanceInput({ ...original, name: 'Otro nombre' }, original, NOW)
    expect(renamed).toMatchObject({ input: { name: 'Otro nombre' } })
    if ('input' in renamed) {
      expect(renamed.input).not.toHaveProperty('starts_at')
      expect(renamed.input).not.toHaveProperty('ends_at')
    }
    // Alargar el fin de una ventana que empezó hace 2 h es válido y solo envía el fin (el inicio no cambió).
    const extended = buildMaintenanceInput({ ...original, endsAt: at(2 * H) }, original, NOW)
    expect(extended).toHaveProperty('input.ends_at')
    if ('input' in extended) expect(extended.input).not.toHaveProperty('starts_at')
  })
  it('al editar, cada fecha se valida solo si cambió', () => {
    const w = {
      id: 'w1', name: 'En curso', starts_at: new Date(NOW - 2 * H).toISOString(), ends_at: new Date(NOW + H).toISOString(), scope: { type: 'group', group_id: 'g1' },
    } as unknown as MaintenanceWindow
    const original = maintenanceToDraft(w, NOW)
    // Mover el inicio a más de 1 h atrás se rechaza; dejarlo donde estaba (hace 2 h) no.
    expect(errorsOf(buildMaintenanceInput({ ...original, startsAt: at(-3 * H) }, original, NOW))).toHaveProperty('startsAt')
    expect(buildMaintenanceInput({ ...original, endsAt: at(3 * H) }, original, NOW)).toHaveProperty('input')
    // El fin al pasado (cambiado) se rechaza.
    expect(errorsOf(buildMaintenanceInput({ ...original, endsAt: at(-30 * 60_000) }, original, NOW))).toHaveProperty('endsAt')
  })
})
