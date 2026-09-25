import { describe, expect, it } from 'vitest'
import { isNavItemVisible, NAV_ITEMS } from './nav-items'

const parches = NAV_ITEMS.find((i) => i.to === '/parches')!
const viewer = { role: 'viewer', all_groups: true, can_updates: false } as const

describe('item Parches del sidebar', () => {
  it('lo ve admin o quien tenga can_updates', () => {
    expect(isNavItemVisible(parches, { ...viewer, role: 'admin' })).toBe(true)
    expect(isNavItemVisible(parches, { ...viewer, can_updates: true })).toBe(true)
  })
  it('no lo ve un viewer sin el permiso ni una sesión ausente', () => {
    expect(isNavItemVisible(parches, viewer)).toBe(false)
    expect(isNavItemVisible(parches, null)).toBe(false)
  })
  it('los demás criterios siguen igual', () => {
    const canales = NAV_ITEMS.find((i) => i.to === '/alertas/canales')!
    expect(isNavItemVisible(canales, { ...viewer, all_groups: false })).toBe(false)
    expect(isNavItemVisible(NAV_ITEMS.find((i) => i.to === '/usuarios')!, viewer)).toBe(false)
  })
})
