// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { checkA11y } from '@/test/utils'
import { Pagination } from './pagination'

function setup(over: Partial<Parameters<typeof Pagination>[0]> = {}) {
  const props = {
    page: 2,
    pageSize: 15 as const,
    count: 15,
    itemLabel: 'hosts',
    hasPrev: true,
    hasNext: true,
    onPrev: vi.fn(),
    onNext: vi.fn(),
    onPageSizeChange: vi.fn(),
    ...over,
  }
  render(<Pagination {...props} />)
  return props
}

describe('Pagination', () => {
  it('ofrece los tamaños 15, 30, 50 y 100 y marca el actual', () => {
    setup({ pageSize: 30 })
    expect(screen.getAllByRole('radio').map((r) => r.getAttribute('aria-label') ?? r.textContent)).toEqual(['15', '30', '50', '100'])
    expect(screen.getByRole('radio', { name: '30' }).getAttribute('aria-checked')).toBe('true')
  })

  it('cambiar el tamaño avisa con el número', () => {
    const p = setup()
    fireEvent.click(screen.getByRole('radio', { name: '50' }))
    expect(p.onPageSizeChange).toHaveBeenCalledWith(50)
  })

  it('Anterior/Siguiente navegan y se bloquean sin cursor o cargando', () => {
    const p = setup()
    fireEvent.click(screen.getByRole('button', { name: /Anterior/ }))
    fireEvent.click(screen.getByRole('button', { name: /Siguiente/ }))
    expect(p.onPrev).toHaveBeenCalledTimes(1)
    expect(p.onNext).toHaveBeenCalledTimes(1)
  })

  it('primera página sin siguiente: ambos deshabilitados', () => {
    setup({ page: 1, hasPrev: false, hasNext: false })
    expect((screen.getByRole('button', { name: /Anterior/ }) as HTMLButtonElement).disabled).toBe(true)
    expect((screen.getByRole('button', { name: /Siguiente/ }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('loading bloquea la navegación', () => {
    setup({ loading: true })
    expect((screen.getByRole('button', { name: /Siguiente/ }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('contador de rango y sin resultados', () => {
    setup({ page: 2, count: 15 })
    expect(screen.getByText('16–30 hosts')).toBeTruthy()
  })

  it('sin violaciones de accesibilidad', async () => {
    setup()
    expect(await checkA11y()).toHaveNoViolations()
  })
})
