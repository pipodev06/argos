import type { ApiError } from '@/lib/api'

const NO_KEY = 'Falta ARGOS_SECRET_KEY en el servidor'

/** Traduce los errores conocidos de la API de perfiles SNMP; null = usar el mensaje del servidor. */
export function mapSnmpProfileError(err: ApiError): string | null {
  switch (err.code) {
    case 'profile_exists':
      return 'Ya existe un perfil con ese nombre.'
    case 'too_many_profiles':
      return 'Se alcanzó el máximo de 100 perfiles.'
    case 'profile_in_use':
      return 'Hay dispositivos usando este perfil'
    case 'profile_not_found':
      return 'El perfil ya no existe.'
    case 'secret_key_missing':
      return NO_KEY
    default:
      return err.status === 503 ? NO_KEY : null
  }
}
