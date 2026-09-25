import { describe, expect, it } from 'vitest'
import type { Device } from '@/features/devices/types/device.types'
import { checkOrigin } from './origin'

const dev = (id: string, collector: string | undefined, group: string | null = null): Device => ({
  id, name: id, kind: 'device', address: '10.0.0.1', collector: collector ? { id: `c-${collector}`, name: collector } : undefined,
  group: group ? { id: group, name: group } : null, status: 'up', status_changed_at: '', last_seen_at: null, evaluated: true, created_at: '',
})
const chk = (type: string, scope: { type: string; group_id?: string; device_id?: string }) =>
  ({ type, scope: { group_id: null, group_name: null, ...scope } }) as never

describe('checkOrigin', () => {
  const devices = [dev('r1', 'pc-a', 'g1'), dev('r2', 'pc-b', 'g1'), dev('r3', 'pc-a', 'g2'), dev('r4', 'pc-c')]
  it('ping de un dispositivo: su colector', () => {
    expect(checkOrigin(chk('ping', { type: 'device', device_id: 'r1' }), devices)).toBe('pc-a')
  })
  it('ping de un grupo: colectores distintos del grupo', () => {
    expect(checkOrigin(chk('ping', { type: 'group', group_id: 'g1' }), devices)).toBe('pc-a, pc-b')
  })
  it('ping de todos: recorta con +N', () => {
    expect(checkOrigin(chk('ping', { type: 'all' }), devices)).toBe('pc-a, pc-b +1')
  })
  it('sin dispositivos en el alcance', () => {
    expect(checkOrigin(chk('ping', { type: 'group', group_id: 'nada' }), devices)).toBe('Sin dispositivos')
  })
  it('viewer (sin colector) o lista no cargada: guion', () => {
    expect(checkOrigin(chk('ping', { type: 'device', device_id: 'r1' }), [dev('r1', undefined)])).toBe('—')
    expect(checkOrigin(chk('ping', { type: 'all' }), undefined)).toBe('—')
  })
  it('alcance host: el propio host', () => {
    expect(checkOrigin(chk('tcp', { type: 'host', host_id: 'h1', host_name: 'mi-pc' } as never), devices)).toBe('mi-pc')
  })
  it('otros tipos: los propios hosts del alcance', () => {
    expect(checkOrigin(chk('tcp', { type: 'all' }), devices)).toBe('Cada host con agente')
    expect(checkOrigin(chk('http', { type: 'group', group_id: 'g1' }), devices)).toBe('Hosts del grupo')
  })
})
