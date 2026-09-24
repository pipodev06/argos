import { ApiError } from '@/lib/api'
import type { TestCategory, TestResult } from '../types/channel.types'

const TEST_CATEGORY_LABELS: Record<TestCategory, string> = {
  dns: 'No se pudo resolver el destino',
  connect: 'No se pudo conectar',
  tls: 'Error de certificado TLS',
  timeout: 'Tiempo de espera agotado',
  status: 'El destino respondió con error',
}

export function describeTestResult(r: TestResult): string {
  if (r.ok) return 'Correcto'
  return r.category ? TEST_CATEGORY_LABELS[r.category] : 'Falló la prueba'
}

/** Traduce los errores conocidos de la API de canales; null = usar el mensaje del servidor. */
export function mapChannelError(err: ApiError): string | null {
  if (err.code === 'secret_key_missing' || err.status === 503) {
    return 'El servidor no tiene ARGOS_SECRET_KEY configurada: pide al administrador del sistema que la defina.'
  }
  if (err.code === 'channel_exists') return 'Ya existe un canal con ese nombre.'
  if (err.code === 'too_many_channels') return 'Se alcanzó el máximo de canales permitido.'
  if (err.status === 400) return `${err.message} Recuerda: solo https y puerto 443; no se aceptan destinos internos.`
  return null
}

export function describeTestError(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 429) {
      return err.retryAfter ? `Demasiadas pruebas. Reintenta en ${err.retryAfter} s.` : 'Demasiadas pruebas. Reintenta en un momento.'
    }
    return mapChannelError(err) ?? err.message
  }
  return 'Falló la prueba'
}
