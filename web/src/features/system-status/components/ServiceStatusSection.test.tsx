// @vitest-environment jsdom
import { cleanup, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { checkA11y, mockApi, renderWithRouter } from '@/test/utils'
import type { SystemStatus } from '../types/systemStatus.types'
import { ServiceAttention } from './ServiceAttention'
import { ServiceStatusSection } from './ServiceStatusSection'

function status(over: Partial<SystemStatus> = {}): SystemStatus {
  return {
    status: 'ok',
    version: '1.2.3',
    started_at: '2026-09-26T10:00:00Z',
    uptime_seconds: 90_000,
    notifications: { pending: 0, oldest_pending_age_seconds: null, dead_24h_total: 0, dead_24h: [] },
    database: { size_bytes: 5 * 1024 * 1024 },
    checks: [
      { name: 'maintenance', status: 'ok', detail: 'Ciclo al día' },
      { name: 'evaluator', status: 'ok', detail: 'Pasada al día' },
      { name: 'notification_queue', status: 'ok', detail: 'Cola vacía' },
      { name: 'notification_dead', status: 'ok', detail: 'Sin entregas fallidas' },
    ],
    ...over,
  }
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('ServiceStatusSection', () => {
  it('ok: muestra versión, tamaño y checks en español; sin entregas fallidas no hay bloque', async () => {
    mockApi({ 'GET /system/status': { data: status() } })
    renderWithRouter(<ServiceStatusSection />)
    expect(await screen.findByText('1.2.3')).toBeTruthy()
    expect(screen.getByText('1 d 1 h')).toBeTruthy()
    expect(screen.getByText('5.0 MB')).toBeTruthy()
    expect(screen.getByText('Cola de notificaciones')).toBeTruthy()
    expect(screen.queryByText('Entregas fallidas en 24 h')).toBeNull()
    expect(await checkA11y()).toHaveNoViolations()
  })

  it('crit: palabra Crítico y lista de canales con enlace a Canales', async () => {
    const dead = [
      { channel_id: 'c1', name: 'Slack ops', owner_group: null, count: 12 },
      { channel_id: 'c2', name: 'Correo', owner_group: { id: 'g1', name: 'Redes' }, count: 3 },
    ]
    mockApi({
      'GET /system/status': {
        data: status({
          status: 'crit',
          checks: [{ name: 'notification_dead', status: 'crit', detail: '15 entregas fallidas' }],
          notifications: { pending: 0, oldest_pending_age_seconds: null, dead_24h_total: 15, dead_24h: dead },
        }),
      },
    })
    renderWithRouter(<ServiceStatusSection />)
    expect((await screen.findAllByText('Crítico')).length).toBeGreaterThan(0)
    expect(screen.getByText('Global')).toBeTruthy()
    expect(screen.getByText('Redes')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Ir a Canales' }).getAttribute('href')).toBe('/alertas/canales')
    expect(await checkA11y()).toHaveNoViolations()
  })

  it('warn: palabra Advertencia', async () => {
    mockApi({ 'GET /system/status': { data: status({ status: 'warn' }) } })
    renderWithRouter(<ServiceStatusSection />)
    expect((await screen.findAllByText('Advertencia')).length).toBeGreaterThan(0)
  })

  it('error: muestra el mensaje', async () => {
    mockApi({ 'GET /system/status': { status: 500, error: { code: 'internal', message: 'Fallo interno' } } })
    renderWithRouter(<ServiceStatusSection />)
    expect(await screen.findByText(/Fallo interno/)).toBeTruthy()
  })
})

describe('ServiceAttention', () => {
  it('solo aparece con warn/crit y enlaza a Sistema', async () => {
    mockApi({ 'GET /system/status': { data: status({ status: 'warn' }) } })
    renderWithRouter(<ServiceAttention />)
    expect((await screen.findByRole('link', { name: 'Ver estado' })).getAttribute('href')).toBe('/sistema')
  })

  it('con ok no muestra nada', async () => {
    const calls = mockApi({ 'GET /system/status': { data: status() } })
    const { container } = renderWithRouter(<ServiceAttention />)
    await vi.waitFor(() => expect(calls.length).toBe(1))
    expect(container.textContent).toBe('')
  })
})
