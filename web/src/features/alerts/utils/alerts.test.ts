import { describe, expect, it } from 'vitest'
import { alertStateLabel, describeCondition, formatAlertValue, formatMetricValue, formatSeconds, parseNumber, scopeLabel } from './format'
import { buildMaintenanceInput, maintenanceToDraft, toLocalInput, type MaintenanceDraft } from './maintenanceForm'
import { ALL_SCOPE, draftToScope, scopeToDraft } from './scope'

describe('scope', () => {
  it('draftToScope', () => {
    expect(draftToScope(ALL_SCOPE)).toEqual({ scope: { type: 'all' } })
    expect(draftToScope({ type: 'group', groupId: '' })).toEqual({ error: 'Elige un grupo.' })
    expect(draftToScope({ type: 'group', groupId: 'g1' })).toEqual({ scope: { type: 'group', group_id: 'g1' } })
  })
  it('scopeToDraft', () => {
    expect(scopeToDraft({ type: 'group', group_id: 'g1', group_name: 'X' })).toEqual({ type: 'group', groupId: 'g1' })
    expect(scopeToDraft({ type: 'all', group_id: null, group_name: null })).toEqual(ALL_SCOPE)
  })
})

describe('format', () => {
  it('formatSeconds usa la unidad exacta más grande', () => {
    expect(formatSeconds(90)).toBe('90 s')
    expect(formatSeconds(600)).toBe('10 min')
    expect(formatSeconds(7200)).toBe('2 h')
    expect(formatSeconds(259200)).toBe('3 d')
  })
  it('parseNumber admite coma y rechaza vacíos', () => {
    expect(parseNumber('1,5')).toBe(1.5)
    expect(parseNumber(' 80 ')).toBe(80)
    expect(parseNumber('')).toBeNull()
    expect(parseNumber('abc')).toBeNull()
  })
  it('formatMetricValue', () => {
    expect(formatMetricValue('metric', 'cpu_pct', null)).toBe('—')
    expect(formatMetricValue('no_data', null, 600)).toBe('10 min')
    expect(formatMetricValue('metric', 'cpu_pct', 90)).toContain('%')
    expect(formatMetricValue('metric', 'load1', 2)).toBe('2')
  })
  it('formatAlertValue muestra pico solo si difiere', () => {
    const a = { kind: 'metric', metric: 'cpu_pct', trigger_value: 90, peak_value: 90 } as const
    expect(formatAlertValue(a)).not.toContain('pico')
    expect(formatAlertValue({ ...a, peak_value: 95 })).toContain('pico')
    expect(formatAlertValue({ ...a, peak_value: null })).not.toContain('pico')
  })
  it('alertStateLabel: mantenimiento solo si no está resuelta', () => {
    expect(alertStateLabel({ state: 'open', suppressed: true })).toBe('En mantenimiento')
    expect(alertStateLabel({ state: 'resolved', suppressed: true })).toBe('Resuelta')
    expect(alertStateLabel({ state: 'open', suppressed: false })).toBe('Abierta')
  })
  it('describeCondition y scopeLabel', () => {
    expect(describeCondition({ kind: 'host_status', metric: null, op: 'gt', threshold: null, duration_seconds: 0 })).toBe('Host caído')
    expect(describeCondition({ kind: 'host_status', metric: null, op: 'gt', threshold: null, duration_seconds: 300 })).toBe('Host caído durante 5 min')
    expect(describeCondition({ kind: 'metric', metric: 'cpu_pct', op: 'gt', threshold: 90, duration_seconds: 600 })).toContain('CPU >')
    expect(describeCondition({ kind: 'metric', metric: 'battery_pct', op: 'lt', threshold: 20, duration_seconds: 0 })).toContain('Batería <')
    expect(scopeLabel({ type: 'all', group_id: null, group_name: null })).toBe('Todos los hosts')
    expect(scopeLabel({ type: 'group', group_id: 'g', group_name: null })).toBe('Grupo: eliminado')
  })
})

describe('buildMaintenanceInput', () => {
  const NOW = new Date(2026, 8, 24, 12, 0).getTime()
  const H = 3_600_000
  const local = (ms: number) => toLocalInput(new Date(ms))
  const draft = (over: Partial<MaintenanceDraft> = {}): MaintenanceDraft => ({
    name: 'Parche',
    startsAt: local(NOW),
    endsAt: local(NOW + H),
    scope: ALL_SCOPE,
    ...over,
  })
  const errorsOf = (r: ReturnType<typeof buildMaintenanceInput>) => ('errors' in r ? r.errors : {})

  it('borrador por defecto: 1 h desde ahora, todos los hosts', () => {
    expect(maintenanceToDraft(null, NOW)).toEqual({ name: '', startsAt: local(NOW), endsAt: local(NOW + H), scope: ALL_SCOPE })
  })
  it('válido envía nombre, alcance y fechas ISO', () => {
    const r = buildMaintenanceInput(draft({ name: '  Parche  ' }), null, NOW)
    expect(r).toMatchObject({ input: { name: 'Parche', scope: { type: 'all' } } })
    expect('input' in r && r.input.starts_at).toMatch(/Z$/)
  })
  it('nombre vacío y grupo sin elegir', () => {
    const e = errorsOf(buildMaintenanceInput(draft({ name: ' ', scope: { type: 'group', groupId: '' } }), null, NOW))
    expect(e.name).toBeTruthy()
    expect(e.scope).toBeTruthy()
  })
  it('fechas faltantes o invertidas', () => {
    expect(errorsOf(buildMaintenanceInput(draft({ startsAt: '' }), null, NOW)).startsAt).toBeTruthy()
    expect(errorsOf(buildMaintenanceInput(draft({ endsAt: local(NOW - H) }), null, NOW)).endsAt).toContain('posterior')
  })
  it('topes: 24 h sobre todos, 31 d en grupo', () => {
    expect(errorsOf(buildMaintenanceInput(draft({ endsAt: local(NOW + 25 * H) }), null, NOW)).endsAt).toContain('24 horas')
    const group = { scope: { type: 'group', groupId: 'g' } } as const
    expect(errorsOf(buildMaintenanceInput(draft({ ...group, endsAt: local(NOW + 30 * 24 * H) }), null, NOW))).toEqual({})
    expect(errorsOf(buildMaintenanceInput(draft({ ...group, endsAt: local(NOW + 32 * 24 * H) }), null, NOW)).endsAt).toContain('31')
  })
  it('al crear no acepta fin pasado ni inicio de hace más de 1 h', () => {
    expect(errorsOf(buildMaintenanceInput(draft({ startsAt: local(NOW - 3 * H), endsAt: local(NOW - H) }), null, NOW)).endsAt).toContain('pasó')
    expect(errorsOf(buildMaintenanceInput(draft({ startsAt: local(NOW - 2 * H), endsAt: local(NOW + H) }), null, NOW)).startsAt).toBeTruthy()
  })
  it('en edición sin cambiar fechas no las envía ni las revalida', () => {
    const d = draft({ startsAt: local(NOW - 5 * H), endsAt: local(NOW - 4 * H), name: 'Nuevo' })
    const r = buildMaintenanceInput(d, { ...d, name: 'Viejo' }, NOW)
    expect(r).toEqual({ input: { name: 'Nuevo', scope: { type: 'all' } } })
  })
})
