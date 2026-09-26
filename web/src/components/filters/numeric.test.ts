import { describe, expect, it } from 'vitest'
import { formatWithUnits, numericChipText } from './numeric'
import type { NumericField } from './types'

const UNITS = [
  { id: 'min', label: 'Minutos', one: 'minuto', many: 'minutos', factor: 60 },
  { id: 'h', label: 'Horas', one: 'hora', many: 'horas', factor: 3600 },
  { id: 'd', label: 'Días', one: 'día', many: 'días', factor: 86400 },
]

describe('formatWithUnits', () => {
  it('usa la unidad más grande que divide exacto', () => {
    expect(formatWithUnits(86400, UNITS, 's')).toBe('1 día')
    expect(formatWithUnits(7200, UNITS, 's')).toBe('2 horas')
    expect(formatWithUnits(5400, UNITS, 's')).toBe('90 minutos')
  })
  it('sin división exacta usa la unidad base', () => {
    expect(formatWithUnits(61, UNITS, 's')).toBe('61 s')
    expect(formatWithUnits(30, UNITS)).toBe('30')
  })
})

describe('numericChipText', () => {
  const base = { kind: 'numeric', key: 'k', label: 'CPU', extra: true, op: '>' } as const satisfies NumericField
  it('con sufijo, con unidades y sin nada', () => {
    expect(numericChipText({ ...base, suffix: '%' }, 80)).toBe('CPU > 80 %')
    expect(numericChipText({ ...base, label: 'Visto hace', units: UNITS, baseUnit: 's' }, 7200)).toBe('Visto hace > 2 horas')
    expect(numericChipText(base, 5)).toBe('CPU > 5')
    expect(numericChipText({ ...base, label: 'Días al fin de vida', op: '≤', suffix: 'd' }, 90)).toBe('Días al fin de vida ≤ 90 d')
  })
})
