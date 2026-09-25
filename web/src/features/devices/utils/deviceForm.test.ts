import { describe, expect, it } from 'vitest'
import { ApiError } from '@/lib/api'
import type { Device } from '../types/device.types'
import { buildDeviceInput, isIpAddress, mapDeviceError, MAX_NAME } from './deviceForm'
import { deviceStateOverride } from './status'

const device: Device = {
  id: 'd1',
  name: 'Switch',
  kind: 'device',
  address: '10.0.0.5',
  collector: { id: 'c1', name: 'srv' },
  group: { id: 'g1', name: 'Red' },
  status: 'up',
  status_changed_at: '2026-09-25T10:00:00Z',
  last_seen_at: null,
  evaluated: true,
  created_at: '2026-09-25T10:00:00Z',
}

describe('isIpAddress', () => {
  it.each(['10.0.0.5', '192.168.1.254', ' 8.8.8.8 ', '::1', '2001:db8::1', 'fe80::1', '1:2:3:4:5:6:7:8', '::ffff:10.0.0.1', '::'])(
    'acepta %s',
    (v) => expect(isIpAddress(v)).toBe(true),
  )
  it.each(['', 'router.local', '10.0.0', '10.0.0.256', '10.0.0.5.1', '01.2.3.4', 'fe80::1%eth0', '1:2:3:4:5:6:7', '1::2::3', '1:2:3:4:5:6:7:8:9', 'g::1', '12345::1', '10.0.0.5/24'])(
    'rechaza "%s"',
    (v) => expect(isIpAddress(v)).toBe(false),
  )
})

describe('buildDeviceInput', () => {
  const draft = { name: ' Switch ', address: ' 10.0.0.5 ', collectorId: 'c1', groupId: '', snmpProfileId: '' }

  it('crear: cuerpo completo sin group_id si no hay grupo', () => {
    expect(buildDeviceInput(draft, null)).toEqual({ input: { name: 'Switch', address: '10.0.0.5', collector_id: 'c1' } })
  })
  it('crear: con grupo', () => {
    expect(buildDeviceInput({ ...draft, groupId: 'g1' }, null)).toEqual({
      input: { name: 'Switch', address: '10.0.0.5', collector_id: 'c1', group_id: 'g1' },
    })
  })
  it('valida nombre, IP y colector', () => {
    const r = buildDeviceInput({ name: '', address: 'router', collectorId: '', groupId: '', snmpProfileId: '' }, null)
    expect(r).toHaveProperty('errors')
    if ('errors' in r) expect(Object.keys(r.errors).sort()).toEqual(['address', 'collectorId', 'name'])
    const long = buildDeviceInput({ ...draft, name: 'x'.repeat(MAX_NAME + 1) }, null)
    expect('errors' in long && long.errors.name).toBeTruthy()
  })
  it('editar: solo envia lo que cambio y group_id null quita el grupo', () => {
    expect(buildDeviceInput({ ...draft, address: '10.0.0.6', groupId: 'g1' }, device)).toEqual({ input: { address: '10.0.0.6' } })
    expect(buildDeviceInput({ name: 'Switch', address: '10.0.0.5', collectorId: 'c1', groupId: '', snmpProfileId: '' }, device)).toEqual({
      input: { group_id: null },
    })
  })
  it('perfil SNMP: crear lo envía; editar envía uuid o null para quitarlo', () => {
    expect(buildDeviceInput({ ...draft, snmpProfileId: 'p1' }, null)).toEqual({
      input: { name: 'Switch', address: '10.0.0.5', collector_id: 'c1', snmp_profile_id: 'p1' },
    })
    const withProfile = { ...device, snmp_profile: { id: 'p1', name: 'Core', version: 3 as const } }
    expect(buildDeviceInput({ ...draft, groupId: 'g1', snmpProfileId: 'p2' }, withProfile)).toEqual({ input: { snmp_profile_id: 'p2' } })
    expect(buildDeviceInput({ ...draft, groupId: 'g1', snmpProfileId: '' }, withProfile)).toEqual({ input: { snmp_profile_id: null } })
  })
  it('editar sin cambios: error, no un PATCH vacio', () => {
    expect(buildDeviceInput({ name: 'Switch', address: '10.0.0.5', collectorId: 'c1', groupId: 'g1', snmpProfileId: '' }, device)).toHaveProperty('errors')
  })
})

describe('mapDeviceError', () => {
  it('traduce los codigos del servidor', () => {
    for (const code of ['device_exists', 'too_many_devices', 'collector_invalid', 'group_not_found']) {
      expect(mapDeviceError(new ApiError(code, 'x', 409))).toMatch(/\S/)
    }
    expect(mapDeviceError(new ApiError('otro', 'x', 500))).toBeNull()
  })
})

describe('deviceStateOverride', () => {
  it('sin evaluar cuando el colector no esta activo', () => {
    expect(deviceStateOverride({ evaluated: false, status: 'offline' })).toEqual({
      label: 'Sin evaluar',
      reason: 'El colector no está activo',
    })
  })
  it('estado normal si se evalua o esta deshabilitado', () => {
    expect(deviceStateOverride({ evaluated: true, status: 'up' })).toBeNull()
    expect(deviceStateOverride({ evaluated: false, status: 'disabled' })).toBeNull()
  })
})
