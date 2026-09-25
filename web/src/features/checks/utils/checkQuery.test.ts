import { describe, expect, it } from 'vitest'
import { parseFilters } from '@/components/filters/urlState'
import type { ServiceCheck } from '../types/check.types'
import { CHECK_FIELDS, filterChecks } from './checkQuery'

// Solo los campos que usa el filtro; el resto del tipo no interviene.
const mk = (name: string, type: string, enabled: boolean) => ({ name, type, enabled }) as unknown as ServiceCheck
const ITEMS = [mk('Web Principal', 'http', true), mk('DB', 'tcp', false), mk('API web', 'http', false)]
const filters = (qs: string) => parseFilters(CHECK_FIELDS, new URLSearchParams(qs))
const names = (qs: string) => filterChecks(ITEMS, filters(qs)).map((c) => c.name)

describe('filterChecks', () => {
  it('sin filtros devuelve todo', () => {
    expect(names('')).toHaveLength(3)
  })
  it('busca por nombre sin distinguir mayúsculas ni espacios de borde', () => {
    expect(names('q=%20WEB%20')).toEqual(['Web Principal', 'API web'])
  })
  it('filtra por tipo y activo, combinados', () => {
    expect(names('type=tcp')).toEqual(['DB'])
    expect(names('enabled=yes')).toEqual(['Web Principal'])
    expect(names('type=http&enabled=no')).toEqual(['API web'])
  })
  it('valores fuera de las opciones se ignoran', () => {
    expect(names('type=ftp&enabled=maybe')).toHaveLength(3)
  })
})
