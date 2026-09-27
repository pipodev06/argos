// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { checkA11y, mockApi } from '@/test/utils'
import { PendingTokensDialog } from './PendingTokensDialog'

afterEach(() => vi.unstubAllGlobals())

const TOKEN = {
  id: 't1',
  hostname: 'web-01',
  group: { id: 'g1', name: 'Cliente A' },
  created_by: 'admin',
  created_at: '2026-09-26T10:00:00Z',
  expires_at: '2026-09-27T10:00:00Z',
  max_uses: 1,
  uses_count: 0,
}
const BULK_TOKEN = { ...TOKEN, id: 't2', hostname: null, max_uses: 50, uses_count: 3 }

describe('PendingTokensDialog', () => {
  it('sin tokens muestra el estado vacío', async () => {
    mockApi({ 'GET /hosts/enroll-tokens': { data: { items: [] } } })
    render(<PendingTokensDialog open onClose={() => {}} />)
    expect(await screen.findByText('Sin tokens pendientes')).toBeTruthy()
  })

  it('revocar pide confirmación, llama DELETE y recarga la lista', async () => {
    let items = [TOKEN]
    const calls = mockApi({
      'GET /hosts/enroll-tokens': () => ({ data: { items } }),
      'DELETE /hosts/enroll-tokens/t1': () => {
        items = []
        return { data: null }
      },
    })
    render(<PendingTokensDialog open onClose={() => {}} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Revocar token de web-01' }))
    fireEvent.click(screen.getByRole('button', { name: 'Revocar' }))
    expect(await screen.findByText('Sin tokens pendientes')).toBeTruthy()
    expect(calls.filter((c) => c.method === 'DELETE').length).toBe(1)
  })

  it('404 al revocar explica que el token ya no existe', async () => {
    mockApi({
      'GET /hosts/enroll-tokens': { data: { items: [TOKEN] } },
      'DELETE /hosts/enroll-tokens/t1': { status: 404, error: { code: 'not_found', message: 'x' } },
    })
    render(<PendingTokensDialog open onClose={() => {}} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Revocar token de web-01' }))
    fireEvent.click(screen.getByRole('button', { name: 'Revocar' }))
    await waitFor(() => expect(screen.getByText(/El token ya no existe/)).toBeTruthy())
  })

  it('un token de alta masiva muestra "Alta masiva" y los usos', async () => {
    mockApi({ 'GET /hosts/enroll-tokens': { data: { items: [BULK_TOKEN] } } })
    render(<PendingTokensDialog open onClose={() => {}} />)
    expect(await screen.findByText('Alta masiva')).toBeTruthy()
    expect(screen.getByText('3 / 50')).toBeTruthy()
  })

  it('sin violaciones de accesibilidad (con datos)', async () => {
    mockApi({ 'GET /hosts/enroll-tokens': { data: { items: [TOKEN] } } })
    render(<PendingTokensDialog open onClose={() => {}} />)
    await screen.findByText('web-01')
    expect(await checkA11y()).toHaveNoViolations()
  })
})
