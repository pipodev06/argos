import { describe, expect, it } from 'vitest'
import type { AlertRule } from '../types/alert.types'
import { formatExpectedPorts, parseExpectedPorts } from './expectedPorts'
import { describeCondition, exposedPortsText } from './format'
import { buildRuleInput, ruleToDraft } from './ruleForm'

describe('parseExpectedPorts', () => {
  it('normaliza mayúsculas y separadores', () => {
    expect(parseExpectedPorts(' TCP/22,\nudp/53  tcp/443 ')).toEqual({
      ports: [{ proto: 'tcp', port: 22 }, { proto: 'udp', port: 53 }, { proto: 'tcp', port: 443 }],
    })
  })
  it('vacío es lista vacía', () => expect(parseExpectedPorts('  ')).toEqual({ ports: [] }))
  it.each(['22', 'icmp/1', 'tcp/0', 'tcp/65536', 'tcp/abc', 'tcp/22/x'])('rechaza %s', (t) => {
    expect(parseExpectedPorts(t)).toHaveProperty('error')
  })
  it('rechaza repetidos y más de 100', () => {
    expect(parseExpectedPorts('tcp/22 TCP/22')).toHaveProperty('error')
    const many = Array.from({ length: 101 }, (_, i) => `tcp/${i + 1}`).join(',')
    expect(parseExpectedPorts(many)).toHaveProperty('error')
    expect(parseExpectedPorts(many.split(',').slice(0, 100).join(','))).toHaveProperty('ports')
  })
  it('formatea de vuelta', () => expect(formatExpectedPorts([{ proto: 'tcp', port: 22 }, { proto: 'udp', port: 53 }])).toBe('tcp/22, udp/53'))
})

describe('regla exposed_port', () => {
  const base = { ...ruleToDraft(null), name: 'Puertos', kind: 'exposed_port' as const, expectedPorts: 'tcp/22, udp/53' }
  it('payload sin métrica, operador ni umbral, con severidad warn', () => {
    const r = buildRuleInput(base, false)
    expect('input' in r && r.input).toMatchObject({ severity: 'warn', expected_ports: [{ proto: 'tcp', port: 22 }, { proto: 'udp', port: 53 }] })
    if ('input' in r) for (const k of ['op', 'threshold', 'clear_threshold', 'metric']) expect(r.input).not.toHaveProperty(k)
  })
  it('lista vacía se envía como [] (PATCH la reemplaza)', () => {
    const r = buildRuleInput({ ...base, expectedPorts: '' }, true)
    expect('input' in r && r.input.expected_ports).toEqual([])
  })
  it('error de puertos inválidos', () => {
    const r = buildRuleInput({ ...base, expectedPorts: 'tcp/0' }, false)
    expect('errors' in r && r.errors.ports).toBeTruthy()
  })
  it('editar precarga la lista y la condición cuenta esperados', () => {
    const rule = { ...ruleToDraft(null), kind: 'exposed_port', expected_ports: [{ proto: 'tcp', port: 22 }], duration_seconds: 0 } as unknown as AlertRule
    expect(ruleToDraft({ ...rule, name: 'x', scope: { type: 'all', group_id: null, group_name: null }, severity: 'warn', enabled: true, resolve_seconds: 60, include_offline: false, warn_or_down: false, check_id: null }).expectedPorts).toBe('tcp/22')
    expect(describeCondition(rule)).toBe('Puerto nuevo expuesto (1 esperados)')
  })
  it('texto de alerta: detalle o solo conteo', () => {
    expect(exposedPortsText({ trigger_value: 2, detail: 'tcp/22, udp/53' })).toBe('Puertos: tcp/22, udp/53')
    expect(exposedPortsText({ trigger_value: 3, detail: null })).toBe('3 puertos nuevos expuestos')
    expect(exposedPortsText({ trigger_value: 1, detail: null })).toBe('1 puerto nuevo expuesto')
  })
})
