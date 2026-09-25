import { describe, expect, it } from 'vitest'
import { describeCondition, formatAlertValue } from './format'
import { buildRuleInput, ruleToDraft } from './ruleForm'

const base = { ...ruleToDraft(null), name: 'Parches' }

describe('reglas de parches: formulario -> payload', () => {
  it('pending_updates: umbral y días, duration_seconds 0, sin metric/op', () => {
    const r = buildRuleInput({ ...base, kind: 'pending_updates', threshold: '3', durationDays: '7' }, false)
    expect(r).toMatchObject({ input: { threshold: 3, duration_days: 7, duration_seconds: 0 } })
    if ('input' in r) expect(r.input).not.toHaveProperty('op')
  })
  it('reboot_pending y os_eol no envían duration_days', () => {
    const a = buildRuleInput({ ...base, kind: 'reboot_pending', threshold: '14' }, false)
    const b = buildRuleInput({ ...base, kind: 'os_eol', threshold: '0' }, false)
    expect(a).toMatchObject({ input: { threshold: 14 } })
    expect(b).toMatchObject({ input: { threshold: 0 } })
    if ('input' in a && 'input' in b) {
      expect(a.input).not.toHaveProperty('duration_days')
      expect(b.input).not.toHaveProperty('duration_days')
    }
  })
  it.each([
    ['pending_updates', '0'], ['pending_updates', '10001'], ['pending_updates', '1.5'],
    ['reboot_pending', '0'], ['reboot_pending', '366'], ['os_eol', '731'], ['os_eol', '-1'], ['os_eol', ''],
  ] as const)('rechaza %s con umbral %s', (kind, threshold) => {
    expect(buildRuleInput({ ...base, kind, threshold }, false)).toHaveProperty('errors.threshold')
  })
  it('rechaza días de duración fuera de 0-365', () => {
    expect(buildRuleInput({ ...base, kind: 'pending_updates', threshold: '1', durationDays: '366' }, false)).toHaveProperty('errors.durationDays')
  })
})

describe('reglas de parches: textos', () => {
  const r = { metric: null, op: null, duration_seconds: 0 }
  it('condición', () => {
    expect(describeCondition({ ...r, kind: 'pending_updates', threshold: 3, duration_days: 7 })).toBe('≥ 3 de seguridad durante 7 d')
    expect(describeCondition({ ...r, kind: 'pending_updates', threshold: 3, duration_days: 0 })).toBe('≥ 3 de seguridad')
    expect(describeCondition({ ...r, kind: 'reboot_pending', threshold: 14 })).toBe('Reinicio pendiente ≥ 14 d')
    expect(describeCondition({ ...r, kind: 'os_eol', threshold: 90 })).toBe('Fin de vida en ≤ 90 d')
  })
  it('valor de la alerta según tipo; detail null = solo el conteo', () => {
    const a = { metric: null, peak_value: 9 }
    expect(formatAlertValue({ ...a, kind: 'pending_updates', trigger_value: 4, detail: null })).toBe('4 de seguridad')
    expect(formatAlertValue({ ...a, kind: 'pending_updates', trigger_value: 4, detail: 'KB1, KB2' })).toBe('4 de seguridad (KB1, KB2)')
    expect(formatAlertValue({ ...a, kind: 'reboot_pending', trigger_value: 15 })).toBe('15 d')
    expect(formatAlertValue({ ...a, kind: 'os_eol', trigger_value: 30 })).toBe('faltan 30 d')
    expect(formatAlertValue({ ...a, kind: 'os_eol', trigger_value: 0 })).toBe('vencido')
    expect(formatAlertValue({ ...a, kind: 'os_eol', trigger_value: -5 })).toBe('vencido')
  })
})
