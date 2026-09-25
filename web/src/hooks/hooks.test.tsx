// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { MemoryRouter, useLocation } from 'react-router'
import { describe, expect, it, vi } from 'vitest'
import type { FilterField } from '@/components/filters/types'
import { useCursorPagination } from './useCursorPagination'
import { useUrlFilters } from './useUrlFilters'

describe('useCursorPagination', () => {
  it('avanza, retrocede y reinicia', () => {
    const { result } = renderHook(() => useCursorPagination())
    expect(result.current).toMatchObject({ cursor: null, page: 1, hasPrev: false, pageSize: 15 })
    act(() => result.current.next('c2'))
    act(() => result.current.next('c3'))
    expect(result.current).toMatchObject({ cursor: 'c3', page: 3, hasPrev: true })
    act(() => result.current.prev())
    expect(result.current).toMatchObject({ cursor: 'c2', page: 2 })
    act(() => result.current.reset())
    expect(result.current).toMatchObject({ cursor: null, page: 1, hasPrev: false })
  })
  it('prev en la primera página no hace nada', () => {
    const { result } = renderHook(() => useCursorPagination())
    act(() => result.current.prev())
    expect(result.current).toMatchObject({ cursor: null, page: 1 })
  })
  it('cambiar el tamaño vuelve a la primera página', () => {
    const { result } = renderHook(() => useCursorPagination())
    act(() => result.current.next('c2'))
    act(() => result.current.setPageSize(50))
    expect(result.current).toMatchObject({ cursor: null, page: 1, pageSize: 50 })
  })
})

const FIELDS = [
  { kind: 'search', key: 'q', label: 'Buscar', ariaLabel: 'Buscar', placeholder: 'Buscar' },
  { kind: 'multi', key: 'status', label: 'Estado', plural: 'estados', options: [{ id: 'up', name: 'Activo' }] },
] as const satisfies readonly FilterField[]

function setup(initial: string, onChange?: () => void) {
  const wrapper = ({ children }: { children: ReactNode }) => <MemoryRouter initialEntries={[initial]}>{children}</MemoryRouter>
  return renderHook(() => ({ f: useUrlFilters(FIELDS, { onChange }), loc: useLocation() }), { wrapper })
}

describe('useUrlFilters', () => {
  it('lee los filtros de la URL', () => {
    const { result } = setup('/hosts?q=web&status=up&status=zzz')
    expect(result.current.f.filters).toEqual({ q: 'web', status: ['up'] })
    expect(result.current.f.hasActive).toBe(true)
  })
  it('setFilters escribe la URL y avisa con onChange', () => {
    const onChange = vi.fn()
    const { result } = setup('/hosts', onChange)
    act(() => result.current.f.setFilters({ status: ['up'] }))
    expect(result.current.loc.search).toBe('?status=up')
    expect(onChange).toHaveBeenCalledTimes(1)
  })
  it('clear vacía los filtros', () => {
    const { result } = setup('/hosts?q=web&status=up')
    act(() => result.current.f.clear())
    expect(result.current.loc.search).toBe('')
    expect(result.current.f.hasActive).toBe(false)
  })
})
