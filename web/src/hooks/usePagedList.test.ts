// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { pageWindow } from '@/lib/pagination'
import { usePagedList } from './usePagedList'

const list = (n: number) => Array.from({ length: n }, (_, i) => i + 1)

describe('pageWindow', () => {
  it('calcula ventana y acota la página', () => {
    expect(pageWindow(40, 1, 15)).toEqual({ page: 1, start: 0, end: 15 })
    expect(pageWindow(40, 3, 15)).toEqual({ page: 3, start: 30, end: 40 })
    expect(pageWindow(40, 9, 15)).toEqual({ page: 3, start: 30, end: 40 })
    expect(pageWindow(0, 1, 15)).toEqual({ page: 1, start: 0, end: 0 })
  })
})

describe('usePagedList', () => {
  it('pagina 15 por defecto, avanza y retrocede', () => {
    const items = list(40)
    const { result } = renderHook(() => usePagedList(items))
    expect(result.current).toMatchObject({ page: 1, pageSize: 15, total: 40, hasPrev: false, hasNext: true })
    expect(result.current.rows).toHaveLength(15)
    act(() => result.current.next())
    act(() => result.current.next())
    expect(result.current.rows).toEqual(list(40).slice(30))
    expect(result.current).toMatchObject({ page: 3, hasNext: false, hasPrev: true })
    act(() => result.current.prev())
    expect(result.current.page).toBe(2)
  })
  it('cambiar el tamaño vuelve a la página 1', () => {
    const items = list(120)
    const { result } = renderHook(() => usePagedList(items))
    act(() => result.current.next())
    act(() => result.current.setPageSize(50))
    expect(result.current).toMatchObject({ page: 1, pageSize: 50, hasNext: true })
    expect(result.current.rows).toHaveLength(50)
  })
  it('si la lista se acorta, la página se acota', () => {
    let items = list(40)
    const { result, rerender } = renderHook(() => usePagedList(items))
    act(() => result.current.next())
    act(() => result.current.next())
    items = list(20)
    rerender()
    expect(result.current).toMatchObject({ page: 2, hasNext: false })
    expect(result.current.rows).toHaveLength(5)
  })
})
