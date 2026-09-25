import type { SnmpAuthProto, SnmpPrivProto, SnmpProfile, SnmpProfileInput, SnmpVersion } from '../types/snmpProfile.types'

export const MAX_NAME = 80
export const MAX_USER = 64
export const MIN_PASS = 8
export const MAX_PASS = 128
export const MAX_COMMUNITY = 64

const USER_RE = /^[A-Za-z0-9._@:-]+$/

export const VERSION_OPTIONS = [
  { value: '3', label: 'SNMPv3 (recomendado)' },
  { value: '2', label: 'SNMPv2c (menos seguro)' },
] as const
export const AUTH_OPTIONS = [
  { value: 'SHA256', label: 'SHA-256' },
  { value: 'SHA512', label: 'SHA-512' },
] as const
export const PRIV_OPTIONS = [
  { value: 'AES128', label: 'AES-128' },
  { value: 'AES256', label: 'AES-256' },
] as const

export const versionLabel = (v: SnmpVersion): string => (v === 3 ? 'SNMPv3' : 'SNMPv2c')

/** Borrador (todo texto). Los secretos son solo de escritura: nunca se precargan al editar. */
export interface ProfileDraft {
  version: '3' | '2'
  name: string
  username: string
  authProto: SnmpAuthProto
  authPass: string
  privProto: SnmpPrivProto
  privPass: string
  allowV2c: boolean
  community: string
}

export type ProfileErrors = Partial<Record<keyof ProfileDraft, string>>

export function profileToDraft(p: SnmpProfile | null): ProfileDraft {
  return {
    version: p ? (String(p.version) as '3' | '2') : '3',
    name: p?.name ?? '',
    username: p?.username ?? '',
    authProto: p?.auth_proto ?? 'SHA256',
    authPass: '',
    privProto: p?.priv_proto ?? 'AES128',
    privPass: '',
    allowV2c: false,
    community: '',
  }
}

/** Longitud en puntos de código (el servidor cuenta runas). */
const len = (s: string): number => [...s].length

function passError(label: string, v: string, required: boolean): string | undefined {
  if (v === '') return required ? `Ingresa la ${label}.` : undefined
  if (len(v) < MIN_PASS || len(v) > MAX_PASS) return `La ${label} debe tener entre ${MIN_PASS} y ${MAX_PASS} caracteres.`
  return undefined
}

/**
 * Valida el borrador y arma el cuerpo. Crear: completo (v3 o v2c). Editar (`original`): solo lo que cambió;
 * un secreto vacío = no cambiar. La versión no se envía al editar.
 */
export function buildProfileInput(d: ProfileDraft, original: SnmpProfile | null): { input: SnmpProfileInput } | { errors: ProfileErrors } {
  const editing = original !== null
  const errors: ProfileErrors = {}
  const name = d.name.trim()
  if (!name) errors.name = 'Ingresa un nombre.'
  else if (len(name) > MAX_NAME) errors.name = `Máximo ${MAX_NAME} caracteres.`

  const input: SnmpProfileInput = {}
  if (!editing || name !== original.name) input.name = name
  if (!editing) input.version = d.version === '3' ? 3 : 2

  if (d.version === '3') {
    const user = d.username.trim()
    if (!user || len(user) > MAX_USER || !USER_RE.test(user)) errors.username = `Usuario: 1 a ${MAX_USER} caracteres A-Z a-z 0-9 . _ @ : -`
    const authErr = passError('contraseña de autenticación', d.authPass, !editing)
    if (authErr) errors.authPass = authErr
    const privErr = passError('contraseña de cifrado', d.privPass, !editing)
    if (privErr) errors.privPass = privErr
    if (!editing || user !== original.username) input.username = user
    if (!editing || d.authProto !== original.auth_proto) input.auth_proto = d.authProto
    if (!editing || d.privProto !== original.priv_proto) input.priv_proto = d.privProto
    if (d.authPass) input.auth_pass = d.authPass
    if (d.privPass) input.priv_pass = d.privPass
  } else {
    if (!editing && !d.allowV2c) errors.allowV2c = 'Confirma que entiendes el riesgo de SNMPv2c.'
    const c = d.community
    if (c === '' && !editing) errors.community = 'Ingresa la comunidad.'
    else if (len(c) > MAX_COMMUNITY) errors.community = `Máximo ${MAX_COMMUNITY} caracteres.`
    if (!editing) input.allow_v2c = true
    if (c) input.community = c
  }

  if (Object.keys(errors).length > 0) return { errors }
  if (editing && Object.keys(input).length === 0) return { errors: { name: 'No hay cambios que guardar.' } }
  return { input }
}
