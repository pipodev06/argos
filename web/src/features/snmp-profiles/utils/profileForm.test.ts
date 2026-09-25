import { describe, expect, it } from 'vitest'
import { ApiError } from '@/lib/api'
import type { SnmpProfile } from '../types/snmpProfile.types'
import { mapSnmpProfileError } from './errors'
import { buildProfileInput, profileToDraft } from './profileForm'

const v3draft = { ...profileToDraft(null), name: ' Core ', username: 'argos', authPass: 'auth-pass-1', privPass: 'priv-pass-1' }
const stored: SnmpProfile = {
  id: 'p1', name: 'Core', version: 3, allow_v2c: false, username: 'argos', level: 'authPriv', auth_proto: 'SHA256', priv_proto: 'AES128',
  has_secret: true, has_community: false, device_count: 2, created_at: '', updated_at: '',
}

describe('perfil SNMP: formulario -> payload', () => {
  it('crear v3', () => {
    expect(buildProfileInput(v3draft, null)).toEqual({
      input: { name: 'Core', version: 3, username: 'argos', auth_proto: 'SHA256', auth_pass: 'auth-pass-1', priv_proto: 'AES128', priv_pass: 'priv-pass-1' },
    })
  })
  it('v3: contraseñas de 8 a 128 y usuario válido', () => {
    const r = buildProfileInput({ ...v3draft, authPass: '1234567', privPass: 'x'.repeat(129), username: 'a b' }, null)
    expect('errors' in r && Object.keys(r.errors).sort()).toEqual(['authPass', 'privPass', 'username'])
    expect('input' in buildProfileInput({ ...v3draft, authPass: 'x'.repeat(128) }, null)).toBe(true)
  })
  it('crear v2c exige la casilla y envía allow_v2c', () => {
    const d = { ...profileToDraft(null), version: '2' as const, name: 'Viejo', community: 'public' }
    const bad = buildProfileInput(d, null)
    expect('errors' in bad && bad.errors.allowV2c).toBeTruthy()
    expect(buildProfileInput({ ...d, allowV2c: true }, null)).toEqual({ input: { name: 'Viejo', version: 2, allow_v2c: true, community: 'public' } })
  })
  it('editar: secretos vacíos no se envían y solo va lo que cambió', () => {
    const d = profileToDraft(stored)
    expect(d.authPass).toBe('')
    expect(buildProfileInput(d, stored)).toEqual({ errors: { name: 'No hay cambios que guardar.' } })
    expect(buildProfileInput({ ...d, name: 'Core 2', authProto: 'SHA512', privPass: 'nueva-clave-1' }, stored)).toEqual({
      input: { name: 'Core 2', auth_proto: 'SHA512', priv_pass: 'nueva-clave-1' },
    })
  })
  it('editar v2c: comunidad vacía = no cambiar, sin version ni allow_v2c', () => {
    const v2 = { ...stored, version: 2 as const, username: null, auth_proto: null, priv_proto: null, has_community: true }
    const d = { ...profileToDraft(v2), community: 'nueva' }
    expect(buildProfileInput(d, v2)).toEqual({ input: { community: 'nueva' } })
  })
})

describe('perfil SNMP: errores de API', () => {
  const e = (code: string, status = 409) => new ApiError(code, 'x', status)
  it('traduce los códigos conocidos', () => {
    expect(mapSnmpProfileError(e('profile_in_use'))).toBe('Hay dispositivos usando este perfil')
    expect(mapSnmpProfileError(e('secret_key_missing', 503))).toBe('Falta ARGOS_SECRET_KEY en el servidor')
    expect(mapSnmpProfileError(e('profile_exists'))).toMatch(/nombre/)
    expect(mapSnmpProfileError(e('too_many_profiles'))).toMatch(/100/)
    expect(mapSnmpProfileError(e('profile_not_found', 422))).toMatch(/ya no existe/)
    expect(mapSnmpProfileError(e('otra', 400))).toBeNull()
  })
})
