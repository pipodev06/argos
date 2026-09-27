// @vitest-environment jsdom
import { fireEvent, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { checkA11y, mockApi, renderWithRouter } from '@/test/utils'
import { AddHostDialog } from './AddHostDialog'

afterEach(() => vi.unstubAllGlobals())

describe('AddHostDialog — aviso de hostname_conflict', () => {
  async function openTokenStep(tokenReply: Record<string, unknown>) {
    mockApi({
      'GET /groups': { data: { items: [] } },
      'POST /hosts/enroll-token': { data: { enroll_token: 'secret-token', expires_at: new Date(Date.now() + 10 * 60_000).toISOString(), ...tokenReply } },
    })
    renderWithRouter(<AddHostDialog open onClose={() => {}} />)
    fireEvent.change(await screen.findByLabelText('Hostname'), { target: { value: 'srv-web-01' } })
    fireEvent.click(screen.getByRole('button', { name: 'Generar token' }))
    await screen.findByText('Token')
  }

  it('sin hostname_conflict (campo ausente) no muestra aviso', async () => {
    await openTokenStep({})
    expect(screen.queryByText(/Ya existe un host/)).toBeNull()
  })

  it('con hostname_conflict muestra el aviso, el estado y el enlace al host', async () => {
    await openTokenStep({ hostname_conflict: { host_id: 'h2', status: 'down' } })
    expect(screen.getByText(/Ya existe un host/)).toBeTruthy()
    expect(screen.getByText(/Caído/)).toBeTruthy()
    const link = screen.getByRole('link', { name: /reactivarlo o re-enrolarlo/ })
    expect(link.getAttribute('href')).toBe('/hosts/h2')
  })

  it('sin violaciones de accesibilidad (con aviso)', async () => {
    await openTokenStep({ hostname_conflict: { host_id: 'h2', status: 'down' } })
    expect(await checkA11y()).toHaveNoViolations()
  })
})
