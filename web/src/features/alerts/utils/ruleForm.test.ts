import { describe, expect, it } from 'vitest'
import type { AlertRule } from '../types/alert.types'
import { buildRuleInput, cloneToDraft, MAX_NAME, ruleToDraft } from './ruleForm'

const base = { ...ruleToDraft(null), name: 'Regla' }
const errorsOf = (r: ReturnType<typeof buildRuleInput>) => ('errors' in r ? r.errors : {})

describe('reglas: borrador por defecto', () => {
  it('es una métrica de CPU activa, con 5 min de duración y 1 min de espera', () => {
    expect(ruleToDraft(null)).toMatchObject({ kind: 'metric', metric: 'cpu_pct', op: 'gt', durationMin: '5', resolveMin: '1', enabled: true, severity: 'warn' })
  })
})

describe('reglas: nombre, duración y espera (comunes)', () => {
  it('rechaza nombre vacío o de más de 80 caracteres', () => {
    expect(errorsOf(buildRuleInput({ ...base, name: '   ', threshold: '90' }, false))).toHaveProperty('name')
    expect(errorsOf(buildRuleInput({ ...base, name: 'x'.repeat(MAX_NAME + 1), threshold: '90' }, false))).toHaveProperty('name')
    expect(buildRuleInput({ ...base, name: 'x'.repeat(MAX_NAME), threshold: '90' }, false)).toHaveProperty('input')
  })
  it('recorta el nombre y convierte minutos a segundos (acepta coma decimal)', () => {
    const r = buildRuleInput({ ...base, name: '  CPU alta ', threshold: '90', durationMin: '2,5', resolveMin: '10' }, false)
    expect(r).toMatchObject({ input: { name: 'CPU alta', duration_seconds: 150, resolve_seconds: 600 } })
  })
  it.each([['-1'], ['1441'], ['abc'], ['']])('rechaza una duración de %s minutos', (durationMin) => {
    expect(errorsOf(buildRuleInput({ ...base, threshold: '90', durationMin }, false))).toHaveProperty('duration')
  })
  it('rechaza una espera de resolución fuera de 0-1440', () => {
    expect(errorsOf(buildRuleInput({ ...base, threshold: '90', resolveMin: '1441' }, false))).toHaveProperty('resolve')
    expect(buildRuleInput({ ...base, threshold: '90', resolveMin: '0' }, false)).toHaveProperty('input')
  })
})

describe('reglas: métrica sobre umbral', () => {
  it('arma op, umbral y alcance; al crear no envía clear_threshold si está vacío', () => {
    const r = buildRuleInput({ ...base, threshold: '90', op: 'gt' }, false)
    expect(r).toMatchObject({ input: { op: 'gt', threshold: 90, severity: 'warn', enabled: true, include_offline: false, scope: { type: 'all' } } })
    if ('input' in r) expect(r.input).not.toHaveProperty('clear_threshold')
  })
  it('al editar, un umbral de recuperación vacío se envía como null (lo borra)', () => {
    expect(buildRuleInput({ ...base, threshold: '90' }, true)).toMatchObject({ input: { clear_threshold: null } })
  })
  it('porcentajes: entre 0 y 100; carga (load): hasta 1 000 000', () => {
    expect(errorsOf(buildRuleInput({ ...base, threshold: '101' }, false))).toHaveProperty('threshold')
    expect(errorsOf(buildRuleInput({ ...base, threshold: '-1' }, false))).toHaveProperty('threshold')
    expect(errorsOf(buildRuleInput({ ...base, threshold: '' }, false))).toHaveProperty('threshold')
    expect(buildRuleInput({ ...base, metric: 'load1', threshold: '250' }, false)).toHaveProperty('input')
    expect(errorsOf(buildRuleInput({ ...base, metric: 'load1', threshold: '1000001' }, false))).toHaveProperty('threshold')
  })
  it('el umbral de recuperación debe quedar del lado sano', () => {
    // gt: recuperar en <= umbral
    expect(errorsOf(buildRuleInput({ ...base, op: 'gt', threshold: '90', clear: '95' }, false))).toHaveProperty('clear')
    expect(buildRuleInput({ ...base, op: 'gt', threshold: '90', clear: '80' }, false)).toMatchObject({ input: { clear_threshold: 80 } })
    expect(buildRuleInput({ ...base, op: 'gt', threshold: '90', clear: '90' }, false)).toMatchObject({ input: { clear_threshold: 90 } })
    // lt: recuperar en >= umbral
    expect(errorsOf(buildRuleInput({ ...base, metric: 'battery_pct', op: 'lt', threshold: '20', clear: '10' }, false))).toHaveProperty('clear')
    expect(buildRuleInput({ ...base, metric: 'battery_pct', op: 'lt', threshold: '20', clear: '30' }, false)).toMatchObject({ input: { clear_threshold: 30 } })
  })
})

describe('reglas: otros tipos', () => {
  it('host_status: sin umbral ni op', () => {
    const r = buildRuleInput({ ...base, kind: 'host_status' }, false)
    expect(r).toHaveProperty('input')
    if ('input' in r) {
      expect(r.input).not.toHaveProperty('threshold')
      expect(r.input).not.toHaveProperty('op')
    }
  })
  it('no_data: segundos entre 60 y 30 días', () => {
    expect(buildRuleInput({ ...base, kind: 'no_data', threshold: '300' }, false)).toMatchObject({ input: { threshold: 300 } })
    expect(errorsOf(buildRuleInput({ ...base, kind: 'no_data', threshold: '59' }, false))).toHaveProperty('threshold')
    expect(errorsOf(buildRuleInput({ ...base, kind: 'no_data', threshold: String(30 * 24 * 3600 + 1) }, false))).toHaveProperty('threshold')
  })
  it('service_check: exige chequeo, hereda el alcance del chequeo y envía warn_or_down', () => {
    expect(errorsOf(buildRuleInput({ ...base, kind: 'service_check' }, false))).toHaveProperty('check')
    const r = buildRuleInput({ ...base, kind: 'service_check', checkId: 'c1', warnOrDown: true }, false)
    expect(r).toMatchObject({ input: { warn_or_down: true } })
    if ('input' in r) {
      expect(r.input).not.toHaveProperty('scope')
      expect(r.input).not.toHaveProperty('include_offline')
    }
  })
  it('exposed_port: envía los puertos esperados y rechaza los inválidos', () => {
    expect(buildRuleInput({ ...base, kind: 'exposed_port', expectedPorts: 'tcp/22, udp/53' }, false)).toMatchObject({
      input: { expected_ports: [{ proto: 'tcp', port: 22 }, { proto: 'udp', port: 53 }] },
    })
    expect(buildRuleInput({ ...base, kind: 'exposed_port', expectedPorts: '' }, false)).toMatchObject({ input: { expected_ports: [] } })
    expect(errorsOf(buildRuleInput({ ...base, kind: 'exposed_port', expectedPorts: 'tcp/99999' }, false))).toHaveProperty('ports')
  })
  it('alcance de grupo sin elegir el grupo: error de alcance', () => {
    expect(errorsOf(buildRuleInput({ ...base, threshold: '90', scope: { type: 'group', groupId: '' } }, false))).toHaveProperty('scope')
  })
})

describe('reglas: ida y vuelta (regla -> borrador -> payload)', () => {
  it('una regla de métrica guardada vuelve a armarse igual', () => {
    const rule = {
      id: 'r1', name: 'CPU', kind: 'metric', metric: 'cpu_pct', op: 'gt', threshold: 90, clear_threshold: 80, escalate_after_seconds: null, escalation_channel_ids: [], duration_seconds: 300,
      duration_days: 0, resolve_seconds: 60, include_offline: true, check_id: null, warn_or_down: false, expected_ports: null,
      scope: { type: 'all' }, severity: 'critical', enabled: false,
    } as unknown as AlertRule
    const d = ruleToDraft(rule)
    expect(d).toMatchObject({ threshold: '90', clear: '80', durationMin: '5', resolveMin: '1', includeOffline: true, severity: 'critical', enabled: false })
    expect(buildRuleInput(d, true)).toMatchObject({
      input: { name: 'CPU', op: 'gt', threshold: 90, clear_threshold: 80, duration_seconds: 300, resolve_seconds: 60, include_offline: true, severity: 'critical', enabled: false },
    })
  })
})

describe('reglas: clonar', () => {
  const metric = {
    id: 'r1', name: 'CPU', kind: 'metric', metric: 'mem_pct', op: 'lt', threshold: 90, clear_threshold: 95, escalate_after_seconds: null, escalation_channel_ids: [], duration_seconds: 300,
    duration_days: 0, resolve_seconds: 120, include_offline: true, check_id: null, warn_or_down: false, expected_ports: null,
    scope: { type: 'all' }, severity: 'critical', enabled: false,
  } as unknown as AlertRule

  it('agrega " (copia)" al nombre y conserva los demás campos', () => {
    expect(cloneToDraft(metric)).toMatchObject({
      name: 'CPU (copia)', kind: 'metric', metric: 'mem_pct', op: 'lt', threshold: '90', clear: '95', durationMin: '5', resolveMin: '2',
      includeOffline: true, severity: 'critical', enabled: false,
    })
  })
  it('recorta el nombre largo para que la copia quepa en 80 caracteres', () => {
    const d = cloneToDraft({ ...metric, name: 'x'.repeat(MAX_NAME) })
    expect(d.name).toHaveLength(MAX_NAME)
    expect(d.name.endsWith(' (copia)')).toBe(true)
  })
  it('no arrastra el id y el cuerpo de creación es válido', () => {
    const d = cloneToDraft(metric)
    expect(d).not.toHaveProperty('id')
    expect(buildRuleInput(d, false)).toMatchObject({ input: { name: 'CPU (copia)', threshold: 90, clear_threshold: 95 } })
  })
  it('conserva chequeo y puertos esperados según el tipo', () => {
    const check = { ...metric, kind: 'service_check', metric: null, check_id: 'c1', warn_or_down: true } as unknown as AlertRule
    expect(cloneToDraft(check)).toMatchObject({ kind: 'service_check', checkId: 'c1', warnOrDown: true })
    const ports = { ...metric, kind: 'exposed_port', metric: null, expected_ports: [{ proto: 'tcp', port: 22 }] } as unknown as AlertRule
    expect(cloneToDraft(ports).expectedPorts).toContain('22')
  })
})

describe('reglas: escalamiento', () => {
  const input = (escalateMin: string, editing = false) => {
    const r = buildRuleInput({ ...base, escalateMin, threshold: '90' }, editing)
    return 'input' in r ? r.input : null
  }
  it('vacío es válido: al crear no se envía, al editar manda null', () => {
    expect(input('')).not.toHaveProperty('escalate_after_seconds')
    expect(input('', true)?.escalate_after_seconds).toBeNull()
  })
  it('rechaza 0, decimales, texto y más de 10080', () => {
    for (const v of ['0', '1.5', 'abc', '10081']) expect(errorsOf(buildRuleInput({ ...base, escalateMin: v, threshold: '90' }, false)).escalate).toBeTruthy()
  })
  it('convierte minutos a segundos en los extremos', () => {
    expect(input('1')?.escalate_after_seconds).toBe(60)
    expect(input('10080')?.escalate_after_seconds).toBe(604800)
  })
  it('ruleToDraft/cloneToDraft rehidratan el plazo en minutos', () => {
    const rule = { ...ruleToDraft(null), name: 'x' } as unknown as AlertRule
    expect(ruleToDraft({ ...rule, name: 'x', kind: 'metric', escalate_after_seconds: 900, duration_seconds: 0, resolve_seconds: 0, scope: { type: 'all' } } as AlertRule).escalateMin).toBe('15')
  })
})
