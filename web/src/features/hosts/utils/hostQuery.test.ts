import { describe, expect, it } from 'vitest'
import { parseFilters } from '@/components/filters/urlState'
import { buildApiQuery, defaultDir, HOST_FIELDS, isVolatileSort, type SortKey } from './hostQuery'

const parse = (qs: string) => parseFilters(HOST_FIELDS, new URLSearchParams(qs))

describe('hostQuery', () => {
  it('dirección por defecto según el orden', () => {
    expect(defaultDir('name')).toBe('asc')
    expect(defaultDir('severity')).toBe('asc')
    expect(defaultDir('cpu')).toBe('desc')
    expect(defaultDir('last_seen')).toBe('desc')
  })
  it('solo cpu/mem/disk son volátiles', () => {
    const volatile: SortKey[] = ['cpu', 'mem', 'disk']
    expect(volatile.every(isVolatileSort)).toBe(true)
    expect(isVolatileSort('name')).toBe(false)
    expect(isVolatileSort('last_seen')).toBe(false)
  })
  it('defaults de la URL', () => {
    expect(parse('')).toMatchObject({ sort: 'severity', dir: 'asc', limit: 25, status: [] })
    expect(parse('sort=mem').dir).toBe('desc')
  })
  it('filtros inválidos se ignoran', () => {
    const f = parse('status=zzz&os=beos&disk_gt=101&sort=x&limit=13')
    expect(f).toMatchObject({ status: [], os: [], disk_gt: null, sort: 'severity', limit: 25 })
  })
  it('buildApiQuery fija sort, dir y limit y agrega cursor', () => {
    const q = buildApiQuery(parse('status=down&cpu_gt=90'), 'abc')
    expect(q.getAll('status')).toEqual(['down'])
    expect(q.get('cpu_gt')).toBe('90')
    expect(q.get('sort')).toBe('severity')
    expect(q.get('dir')).toBe('asc')
    expect(q.get('limit')).toBe('25')
    expect(q.get('cursor')).toBe('abc')
    expect(buildApiQuery(parse(''), null).has('cursor')).toBe(false)
  })
})
