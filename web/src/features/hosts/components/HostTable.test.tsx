// @vitest-environment jsdom
import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { checkA11y, renderWithRouter } from '@/test/utils'
import type { Host } from '../types/host.types'
import { HostTable } from './HostTable'

function host(over: Partial<Host>): Host {
  return {
    id: 'h1',
    hostname: 'web-01',
    kind: 'agent',
    status: 'up',
    status_changed_at: '2026-09-26T10:00:00Z',
    last_seen_at: '2026-09-27T10:00:00Z',
    group: { id: 'g1', name: 'Cliente A' },
    os: 'linux',
    agent_version: '1.2.0',
    agent_outdated: false,
    ip: '10.0.0.5',
    cpu_pct: 12,
    mem_pct: 40,
    disk: { pct: 50, mount: '/' },
    boot_time: null,
    battery: null,
    ...over,
  }
}

function table(hosts: Host[]) {
  return renderWithRouter(
    <HostTable hosts={hosts} page={1} pageSize={15} visible={[]} sort="name" dir="asc" onSort={() => {}} selected={null} onToggle={() => {}} onToggleAll={() => {}} />,
  )
}

describe('HostTable', () => {
  it('el nombre es un enlace al detalle (fila clicable)', () => {
    table([host({}), host({ id: 'h2', hostname: 'db-01' })])
    expect(screen.getByRole('link', { name: 'web-01' }).getAttribute('href')).toBe('/hosts/h1')
    expect(screen.getByRole('link', { name: 'db-01' }).getAttribute('href')).toBe('/hosts/h2')
    expect(screen.getAllByRole('row').length).toBe(3)
  })

  it('columna Agente: versión, y "Desactualizado" (icono + texto) solo si el servidor lo indica', () => {
    table([host({ agent_outdated: true }), host({ id: 'h2', hostname: 'db-01', agent_version: 'dev' })])
    const rows = screen.getAllByRole('row')
    expect(rows[1].textContent).toContain('1.2.0')
    expect(rows[1].textContent).toContain('Desactualizado')
    expect(rows[2].textContent).not.toContain('Desactualizado')
    expect(rows[2].textContent).not.toContain('dev')
  })

  it('sin violaciones de accesibilidad', async () => {
    table([host({ agent_outdated: true }), host({ id: 'h2', hostname: 'db-01', status: 'down' })])
    expect(await checkA11y()).toHaveNoViolations()
  })
})
