// @vitest-environment jsdom
import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { checkA11y } from '@/test/utils'
import { HostHeader } from './HostHeader'
import type { HostDetail } from '../types/host.types'

function makeHost(overrides: Partial<HostDetail> = {}): HostDetail {
  return {
    id: 'h1',
    hostname: 'srv-web-01',
    kind: 'agent',
    os: 'linux',
    arch: 'amd64',
    status: 'up',
    last_seen_at: '2026-09-27T10:00:00Z',
    status_changed_at: '2026-09-27T09:00:00Z',
    created_at: '2026-09-01T00:00:00Z',
    group: null,
    detail_retention_days: null,
    effective_detail_days: 30,
    retention_source: 'global',
    ...overrides,
  }
}

describe('HostHeader', () => {
  it('host activo (agent): muestra "Deshabilitar" y "Re-enrolar"', () => {
    render(<HostHeader host={makeHost()} ip={null} canEdit onEdit={() => {}} onRevoke={() => {}} onReEnroll={() => {}} />)
    expect(screen.getByRole('button', { name: /Deshabilitar/ })).toBeTruthy()
    expect(screen.getByRole('button', { name: /Re-enrolar/ })).toBeTruthy()
  })

  it('host disabled (agent): muestra "Reactivar" y no "Deshabilitar"', () => {
    render(
      <HostHeader
        host={makeHost({ status: 'disabled' })}
        ip={null}
        canEdit
        onEdit={() => {}}
        onRevoke={() => {}}
        onReEnroll={() => {}}
      />,
    )
    expect(screen.getByRole('button', { name: /Reactivar/ })).toBeTruthy()
    expect(screen.queryByRole('button', { name: /Deshabilitar/ })).toBeNull()
  })

  it('dispositivo (kind=device): no muestra Deshabilitar ni Reactivar/Re-enrolar', () => {
    render(
      <HostHeader
        host={makeHost({ kind: 'device' })}
        ip={null}
        canEdit
        onEdit={() => {}}
        onRevoke={() => {}}
        onReEnroll={() => {}}
      />,
    )
    expect(screen.queryByRole('button', { name: /Deshabilitar/ })).toBeNull()
    expect(screen.queryByRole('button', { name: /Re-enrolar/ })).toBeNull()
    expect(screen.queryByRole('button', { name: /Reactivar/ })).toBeNull()
  })

  it('sin permiso de edicion: no muestra ninguna accion', () => {
    render(<HostHeader host={makeHost()} ip={null} canEdit={false} onEdit={() => {}} onRevoke={vi.fn()} onReEnroll={vi.fn()} />)
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('sin violaciones de accesibilidad', async () => {
    render(<HostHeader host={makeHost()} ip="10.0.0.1" canEdit onEdit={() => {}} onRevoke={() => {}} onReEnroll={() => {}} />)
    expect(await checkA11y()).toHaveNoViolations()
  })
})
