// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { checkA11y, mockApi } from '@/test/utils'
import { RevokeHostDialog } from './RevokeHostDialog'
import type { HostDetail } from '../types/host.types'

afterEach(() => vi.unstubAllGlobals())

const HOST: HostDetail = {
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
}

describe('RevokeHostDialog', () => {
  it('confirmar llama POST /hosts/{id}/revoke y avisa onDone', async () => {
    const calls = mockApi({ 'POST /hosts/h1/revoke': { data: null } })
    const onDone = vi.fn()
    render(<RevokeHostDialog host={HOST} onClose={() => {}} onDone={onDone} />)
    fireEvent.click(screen.getByRole('button', { name: 'Deshabilitar' }))
    await waitFor(() => expect(onDone).toHaveBeenCalled())
    expect(calls.some((c) => c.method === 'POST' && c.path === '/hosts/h1/revoke')).toBe(true)
  })

  it('sin violaciones de accesibilidad', async () => {
    render(<RevokeHostDialog host={HOST} onClose={() => {}} onDone={() => {}} />)
    expect(await checkA11y()).toHaveNoViolations()
  })
})
