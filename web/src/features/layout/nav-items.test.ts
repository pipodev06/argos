import { describe, expect, it } from 'vitest'
import { groupBySection, isNavItemVisible, NAV_ITEMS, navItemForPath } from './nav-items'

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

describe('secciones y migas del menú', () => {
  it('agrupa en Monitoreo, Configuración y Administración, con Alertas como ítem plano de Monitoreo', () => {
    const sections = groupBySection(NAV_ITEMS).map(([s]) => s)
    expect(sections).toEqual(['Monitoreo', 'Configuración', 'Administración'])
    const alertas = NAV_ITEMS.find((i) => i.to === '/alertas')!
    expect(alertas.section).toBe('Monitoreo')
    expect(alertas.openAlertsCount).toBe(true)
    expect(NAV_ITEMS.find((i) => i.to === '/sistema')?.section).toBe('Administración')
    expect(NAV_ITEMS.find((i) => i.to === '/perfiles-snmp')?.section).toBe('Configuración')
  })
  it('navItemForPath usa el prefijo más largo y "/" solo exacto', () => {
    expect(navItemForPath('/')?.to).toBe('/')
    expect(navItemForPath('/hosts/abc/metricas')?.to).toBe('/hosts')
    expect(navItemForPath('/dispositivos/abc')?.to).toBe('/dispositivos')
    expect(navItemForPath('/alertas/reglas/nueva')?.to).toBe('/alertas/reglas')
    expect(navItemForPath('/alertas/123')?.to).toBe('/alertas')
  })
})
