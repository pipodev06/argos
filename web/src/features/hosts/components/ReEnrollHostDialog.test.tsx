// @vitest-environment jsdom
import { screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { checkA11y, mockApi, renderWithRouter } from '@/test/utils'
import { ReEnrollHostDialog } from './ReEnrollHostDialog'
import type { HostDetail } from '../types/host.types'

afterEach(() => vi.unstubAllGlobals())

function makeHost(overrides: Partial<HostDetail> = {}): HostDetail {
  return {
    id: 'h1',
    hostname: 'srv-web-01',
    kind: 'agent',
    os: 'linux',
    arch: 'amd64',
    status: 'up',
    last_seen_at: null,
    status_changed_at: '2026-09-27T09:00:00Z',
    created_at: '2026-09-01T00:00:00Z',
    group: null,
    detail_retention_days: null,
    effective_detail_days: 30,
    retention_source: 'global',
    ...overrides,
  }
}

describe('ReEnrollHostDialog', () => {
  it('host activo: llama re-enroll y muestra el token', async () => {
    const calls = mockApi({
      'POST /hosts/h1/re-enroll': { data: { enroll_token: 'tok-123', expires_at: new Date(Date.now() + 10 * 60_000).toISOString() } },
    })
    renderWithRouter(<ReEnrollHostDialog host={makeHost()} onClose={() => {}} />)
    await screen.findByText('Token')
    expect(calls).toEqual([{ method: 'POST', path: '/hosts/h1/re-enroll', body: undefined }])
  })

  it('host disabled: titulo indica reactivacion', async () => {
    mockApi({
      'POST /hosts/h1/re-enroll': { data: { enroll_token: 'tok-123', expires_at: new Date(Date.now() + 10 * 60_000).toISOString() } },
    })
    renderWithRouter(<ReEnrollHostDialog host={makeHost({ status: 'disabled' })} onClose={() => {}} />)
    expect(await screen.findByText(/reactivado/)).toBeTruthy()
  })

  it('sin violaciones de accesibilidad', async () => {
    mockApi({
      'POST /hosts/h1/re-enroll': { data: { enroll_token: 'tok-123', expires_at: new Date(Date.now() + 10 * 60_000).toISOString() } },
    })
    renderWithRouter(<ReEnrollHostDialog host={makeHost()} onClose={() => {}} />)
    await screen.findByText('Token')
    expect(await checkA11y()).toHaveNoViolations()
  })
})
