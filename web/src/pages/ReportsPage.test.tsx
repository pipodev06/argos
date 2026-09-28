// @vitest-environment jsdom
import { fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { checkA11y, mockApi, renderWithRouter } from '@/test/utils'
import { ReportsPage } from './ReportsPage'

afterEach(() => vi.unstubAllGlobals())

const FLEET = { hosts: 0, up_s: 0, down_s: 0, down_maint_s: 0, offline_s: 0, incidents: 0, availability_pct: null, coverage_pct: 0 }

function baseRoutes() {
  return {
    'GET /groups': { data: { items: [], ungrouped: { host_count: 0 }, defaults: { down_after_seconds: 300 } } },
    'GET /reports/availability': { data: { range: 'custom', by: 'host', from: '', to: '', group_id: null, fleet: FLEET, items: [] } },
  }
}

describe('ReportsPage: rango personalizado (B12)', () => {
  it('sin Desde/Hasta no consulta el informe y deshabilita la exportación', async () => {
    const calls = mockApi(baseRoutes())
    renderWithRouter(<ReportsPage />, '/informes?type=availability&range=custom')
    expect(await screen.findByText('Elige "Desde" y "Hasta" para ver el informe.')).toBeTruthy()
    expect(calls.some((c) => c.path === '/reports/availability')).toBe(false)
    const exportBtn = screen.getByRole('button', { name: 'Exportar CSV' }) as HTMLButtonElement
    expect(exportBtn.disabled).toBe(true)
    expect(await checkA11y()).toHaveNoViolations()
  })

  it('con Desde y Hasta ya en la URL, consulta con from/to (no con range)', async () => {
    const calls = mockApi(baseRoutes())
    renderWithRouter(
      <ReportsPage />,
      '/informes?type=availability&range=custom&from=2026-08-01T00%3A00%3A00Z&to=2026-08-08T00%3A00%3A00Z',
    )
    await waitFor(() => expect(calls.some((c) => c.path === '/reports/availability')).toBe(true))
    const call = calls.find((c) => c.path === '/reports/availability')
    expect(call).toBeDefined()
    const link = (await screen.findByRole('link', { name: /Exportar CSV/ })) as HTMLAnchorElement
    expect(link.getAttribute('href')).toContain('from=2026-08-01T00%3A00%3A00Z')
    expect(link.getAttribute('href')).not.toContain('range=custom')
  })

  it('escribir Desde y Hasta pasa de "elige fechas" a consultar el informe', async () => {
    mockApi(baseRoutes())
    renderWithRouter(<ReportsPage />, '/informes?type=availability&range=custom')
    await screen.findByText('Elige "Desde" y "Hasta" para ver el informe.')
    fireEvent.change(screen.getByLabelText('Desde'), { target: { value: '2026-08-01T00:00' } })
    fireEvent.change(screen.getByLabelText('Hasta'), { target: { value: '2026-08-08T00:00' } })
    await waitFor(() => expect(screen.queryByText('Elige "Desde" y "Hasta" para ver el informe.')).toBeNull())
  })
})
