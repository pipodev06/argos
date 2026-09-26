import { describe, expect, it } from 'vitest'
import { buildAlertQuery, type AlertFilters } from './alertQuery'

const filters = (over: Partial<AlertFilters> = {}): AlertFilters =>
  ({ state: [], severity: [], rules: [], limit: 15, ...over }) as unknown as AlertFilters

describe('consulta de alertas', () => {
  it('sin filtros solo envía el tamaño de página', () => {
    expect(buildAlertQuery(filters(), null).toString()).toBe('limit=15')
  })
  it('estado y severidad viajan como parámetros repetidos', () => {
    const q = buildAlertQuery(filters({ state: ['open', 'acknowledged'], severity: ['critical'] } as Partial<AlertFilters>), null)
    expect(q.getAll('state')).toEqual(['open', 'acknowledged'])
    expect(q.getAll('severity')).toEqual(['critical'])
  })
  it('las reglas van con el parámetro rule_id (no "rules")', () => {
    const q = buildAlertQuery(filters({ rules: ['r1', 'r2'] } as Partial<AlertFilters>), null)
    expect(q.getAll('rule_id')).toEqual(['r1', 'r2'])
    expect(q.has('rules')).toBe(false)
  })
  it('el cursor solo se envía si hay', () => {
    expect(buildAlertQuery(filters({ limit: 50 } as Partial<AlertFilters>), 'abc').toString()).toContain('cursor=abc')
    expect(buildAlertQuery(filters(), null).has('cursor')).toBe(false)
    expect(buildAlertQuery(filters({ limit: 100 } as Partial<AlertFilters>), null).get('limit')).toBe('100')
  })
})
