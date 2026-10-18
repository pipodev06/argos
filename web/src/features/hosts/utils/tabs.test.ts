import { describe, expect, it } from 'vitest'
import { hostTabPath, legacyTabPath, tabForPath, visibleHostTabs } from './tabs'

const viewer = { role: 'viewer', can_updates: false, can_inventory: false } as const

describe('pestañas del host', () => {
  it('Dispositivos solo en colectores; Parches e Inventario según permiso', () => {
    const ids = (u: Parameters<typeof visibleHostTabs>[0], c: boolean) => visibleHostTabs(u, c).map((t) => t.id)
    expect(ids(viewer, false)).toEqual(['resumen', 'metricas', 'chequeos', 'historial'])
    expect(ids({ ...viewer, role: 'admin' }, true)).toEqual(['resumen', 'metricas', 'chequeos', 'dispositivos', 'parches', 'inventario', 'historial'])
    expect(ids({ ...viewer, can_updates: true }, false)).toContain('parches')
  })
  it('rutas de cada pestaña y redirección de ?tab= viejo', () => {
    expect(hostTabPath('h1', '')).toBe('/hosts/h1')
    expect(legacyTabPath('h1', 'parches')).toBe('/hosts/h1/parches')
    expect(legacyTabPath('h1', 'resumen')).toBe('/hosts/h1')
    expect(legacyTabPath('h1', 'nada')).toBeNull()
    expect(legacyTabPath('h1', null)).toBeNull()
  })
  it('tabForPath detecta la pestaña por la URL', () => {
    expect(tabForPath('h1', '/hosts/h1/metricas').id).toBe('metricas')
    expect(tabForPath('h1', '/hosts/h1').id).toBe('resumen')
    expect(tabForPath('h1', '/hosts/h1/historial/').id).toBe('historial')
  })
})
