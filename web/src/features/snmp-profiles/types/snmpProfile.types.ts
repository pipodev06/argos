export type SnmpVersion = 3 | 2
export type SnmpAuthProto = 'SHA256' | 'SHA512'
export type SnmpPrivProto = 'AES128' | 'AES256'

/** Perfil SNMP (solo admin). Los secretos nunca llegan: solo `has_secret`/`has_community`. */
export interface SnmpProfile {
  id: string
  name: string
  version: SnmpVersion
  allow_v2c: boolean
  username: string | null
  level: 'authPriv' | null
  auth_proto: SnmpAuthProto | null
  priv_proto: SnmpPrivProto | null
  has_secret: boolean
  has_community: boolean
  device_count: number
  created_at: string
  updated_at: string
}

/** Cuerpo de crear (v3 o v2c) o editar (parcial; la versión no cambia). Los secretos son solo de escritura. */
export interface SnmpProfileInput {
  name?: string
  version?: SnmpVersion
  allow_v2c?: boolean
  username?: string
  auth_proto?: SnmpAuthProto
  auth_pass?: string
  priv_proto?: SnmpPrivProto
  priv_pass?: string
  community?: string
}

/** Referencia al perfil en el DTO de un dispositivo. */
export interface SnmpProfileRef {
  id: string
  name: string
  version: SnmpVersion
}
