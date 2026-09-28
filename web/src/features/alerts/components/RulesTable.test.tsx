// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import type { AlertRule } from '../types/alert.types'
import { RulesTable } from './RulesTable'

function makeRule(over: Partial<AlertRule> = {}): AlertRule {
  return {
    id: 'r1',
    name: 'CPU alto',
    kind: 'metric',
    metric: 'cpu_pct',
    op: 'gt',
    threshold: 90,
    clear_threshold: null,
    duration_seconds: 300,
    duration_days: 0,
    resolve_seconds: 60,
    include_offline: false,
    check_id: null,
    check_name: null,
    warn_or_down: false,
    scope: { type: 'all', group_id: null, group_name: null },
    severity: 'warn',
    enabled: true,
    active_alerts: 0,
    channel_ids: [],
    muted_until: null,
    ...over,
  }
}

function setup(rule: AlertRule) {
  const onMute = vi.fn()
  const onUnmute = vi.fn()
  const onAction = vi.fn()
  render(<RulesTable rules={[rule]} canAct busy={false} onAction={onAction} onMute={onMute} onUnmute={onUnmute} />)
  return { onMute, onUnmute, onAction }
}

describe('RulesTable: silenciar una regla', () => {
  it('sin silenciar: muestra "Silenciar" y llama a onMute con los minutos elegidos', async () => {
    const { onMute } = setup(makeRule())
    expect(screen.queryByRole('button', { name: /Quitar silencio/ })).toBeNull()
    fireEvent.pointerDown(screen.getByRole('button', { name: /Silenciar regla/ }), { button: 0, ctrlKey: false })
    fireEvent.click(await screen.findByText('1 h'))
    expect(onMute).toHaveBeenCalledTimes(1)
    expect(onMute.mock.calls[0][1]).toBe(60)
  })

  it('silenciada (muted_until en el futuro): muestra "Quitar silencio" y el badge, y llama a onUnmute', () => {
    const mutedUntil = new Date(Date.now() + 60 * 60 * 1000).toISOString()
    const { onUnmute } = setup(makeRule({ muted_until: mutedUntil }))
    expect(screen.getByText(/Silenciada hasta/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /Quitar silencio/ }))
    expect(onUnmute).toHaveBeenCalledTimes(1)
    expect(onUnmute.mock.calls[0][0]).toMatchObject({ id: 'r1' })
  })

  it('muted_until en el pasado se trata como no silenciada', () => {
    const past = new Date(Date.now() - 60 * 1000).toISOString()
    setup(makeRule({ muted_until: past }))
    expect(screen.queryByRole('button', { name: /Quitar silencio/ })).toBeNull()
    expect(screen.getByRole('button', { name: /Silenciar regla/ })).toBeTruthy()
  })
})
