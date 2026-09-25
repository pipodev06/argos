import { describe, expect, it } from 'vitest'
import type { FilterField } from './types'
import { clearedFilters, hasActiveFilters, parseFilters, serializeFilters } from './urlState'

const FIELDS = [
  { kind: 'search', key: 'q', label: 'Buscar', ariaLabel: 'Buscar', placeholder: 'Buscar' },
  { kind: 'datetime', key: 'from', label: 'Desde' },
  { kind: 'multi', key: 'status', label: 'Estado', plural: 'estados', options: [{ id: 'up', name: 'Activo' }, { id: 'down', name: 'Caído' }] },
  { kind: 'numeric', key: 'cpu_gt', label: 'CPU', extra: true, op: '>', max: 100, suffix: '%' },
  { kind: 'numeric', key: 'seen', label: 'Visto', extra: true, op: '>', integer: true },
  { kind: 'choice', key: 'sort', label: 'Orden', values: ['name', 'cpu'], default: 'name' },
  { kind: 'choice', key: 'dir', label: 'Dir', values: ['asc', 'desc'], default: (v: Record<string, unknown>) => (v.sort === 'cpu' ? 'desc' : 'asc') },
  { kind: 'choice', key: 'limit', label: 'Límite', values: [25, 50], default: 25 },
] as const satisfies readonly FilterField[]

const parse = (qs: string) => parseFilters(FIELDS, new URLSearchParams(qs))

describe('parseFilters', () => {
  it('URL vacía da valores por defecto', () => {
    expect(parse('')).toEqual({ q: '', from: '', status: [], cpu_gt: null, seen: null, sort: 'name', dir: 'asc', limit: 25 })
  })

  it('lee valores válidos', () => {
    const f = parse('q=web&status=up&status=down&cpu_gt=80&sort=cpu&limit=50')
    expect(f).toMatchObject({ q: 'web', status: ['up', 'down'], cpu_gt: 80, sort: 'cpu', dir: 'desc', limit: 50 })
  })

  it('ignora valores inválidos', () => {
    const f = parse('status=zzz&status=up&status=up&cpu_gt=150&seen=-3&sort=hack&limit=7&from=nope')
    expect(f.status).toEqual(['up'])
    expect(f.cpu_gt).toBeNull()
    expect(f.seen).toBeNull()
    expect(f.sort).toBe('name')
    expect(f.limit).toBe(25)
    expect(f.from).toBe('')
  })

  it('numérico: rechaza no numéricos y trunca enteros', () => {
    expect(parse('cpu_gt=abc').cpu_gt).toBeNull()
    expect(parse('cpu_gt=').cpu_gt).toBeNull()
    expect(parse('cpu_gt=0').cpu_gt).toBe(0)
    expect(parse('seen=7.9').seen).toBe(7)
  })
})

describe('serializeFilters', () => {
  it('omite vacíos y valores por defecto', () => {
    expect(serializeFilters(FIELDS, parse('')).toString()).toBe('')
  })

  it('ida y vuelta conserva los filtros', () => {
    const f = parse('q=web&status=up&status=down&cpu_gt=80&seen=3&sort=cpu&dir=asc&limit=50&from=2026-09-24T10:00:00Z')
    expect(parse(serializeFilters(FIELDS, f).toString())).toEqual(f)
  })

  it('dir por defecto depende de sort', () => {
    expect(serializeFilters(FIELDS, parse('sort=cpu&dir=desc')).toString()).toBe('sort=cpu')
    expect(serializeFilters(FIELDS, parse('sort=cpu&dir=asc')).toString()).toBe('sort=cpu&dir=asc')
  })
})

describe('hasActiveFilters / clearedFilters', () => {
  it('los choice no cuentan como filtro activo', () => {
    expect(hasActiveFilters(FIELDS, parse('sort=cpu&limit=50'))).toBe(false)
    expect(hasActiveFilters(FIELDS, parse('cpu_gt=0'))).toBe(true)
    expect(hasActiveFilters(FIELDS, parse('status=up'))).toBe(true)
  })

  it('clear vacía filtros y conserva choice', () => {
    const cleared = clearedFilters(FIELDS, parse('q=x&status=up&cpu_gt=5&sort=cpu&limit=50'))
    expect(cleared).toMatchObject({ q: '', status: [], cpu_gt: null, sort: 'cpu', limit: 50 })
    expect(hasActiveFilters(FIELDS, cleared)).toBe(false)
  })
})
