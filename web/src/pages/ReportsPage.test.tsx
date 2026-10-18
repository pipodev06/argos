// @vitest-environment jsdom
import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { checkA11y, mockApi, renderWithRouter } from '@/test/utils'
import { ReportsPage } from './ReportsPage'

// El calendario es lazy (carga al abrir el popover): se precarga el modulo para no medir su transformacion en el test.
beforeAll(async () => {
  await import('@/components/ui/date-calendar')
}, 60_000)

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
    expect(await screen.findByText('Elige Desde y Hasta')).toBeTruthy()
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

  it('elegir Desde y Hasta en el calendario pasa de "elige fechas" a consultar el informe', async () => {
    // Solo se falsea Date: los dias futuros estan deshabilitados y el test elige el 10 y el 20.
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date(2026, 9, 25, 12, 0))
    mockApi(baseRoutes())
    renderWithRouter(<ReportsPage />, '/informes?type=availability&range=custom')
    await screen.findByText('Elige Desde y Hasta')
    for (const [label, day] of [['Desde', '10'], ['Hasta', '20']] as const) {
      fireEvent.click(screen.getByRole('button', { name: label }))
      fireEvent.click(within(await screen.findByRole('grid', {}, { timeout: 4000 })).getByText(day))
      fireEvent.click(screen.getByRole('button', { name: 'Listo' }))
    }
    await waitFor(() => expect(screen.queryByText('Elige Desde y Hasta')).toBeNull())
    vi.useRealTimers()
  }, 30_000)
})
