// Cliente HTTP centralizado: rutas relativas (proxy de Vite en dev), envelope { data, error }.
// La sesion es una cookie HttpOnly que maneja el navegador: JS nunca la lee ni la guarda.
interface ApiErrorBody {
  code: string
  message: string
  /** Datos extra del error (p. ej. cuentas de un 409); su forma depende del codigo. */
  details?: unknown
}

interface ApiEnvelope<T> {
  data: T | null
  error: ApiErrorBody | null
}

export class ApiError extends Error {
  readonly code: string
  readonly status: number
  /** Segundos indicados por la cabecera Retry-After (429), si vino en formato numerico. */
  readonly retryAfter?: number
  /** `error.details` del servidor, si vino. */
  readonly details?: unknown

  constructor(code: string, message: string, status = 0, retryAfter?: number, details?: unknown) {
    super(message)
    this.name = 'ApiError'
    this.code = code
    this.status = status
    this.retryAfter = retryAfter
    this.details = details
  }
}

export const UNAUTHENTICATED = 'unauthenticated'

const FORBIDDEN_MESSAGE = 'No tienes permiso para realizar esta acción.'

// Manejo central de 401 `unauthenticated` (sesion ausente o expirada): la capa de auth se suscribe
// una vez y limpia la sesion. Los 401 `invalid_credentials` (login / cambio de clave) no cuentan.
type UnauthorizedListener = () => void
const unauthorizedListeners = new Set<UnauthorizedListener>()

export function onUnauthorized(listener: UnauthorizedListener): () => void {
  unauthorizedListeners.add(listener)
  return () => {
    unauthorizedListeners.delete(listener)
  }
}

export function isUnauthenticated(err: unknown): boolean {
  return err instanceof ApiError && err.code === UNAUTHENTICATED
}

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
  body?: unknown
  signal?: AbortSignal
  /** Respuestas 200 sin datos utiles (logout, cambio de clave): no exige `data`. */
  noData?: boolean
}

function parseRetryAfter(res: Response): number | undefined {
  const raw = res.headers.get('Retry-After')
  if (!raw || !/^\d+$/.test(raw.trim())) return undefined
  return Number(raw)
}

export async function apiRequest<T>(
  path: string,
  { method = 'GET', body, signal, noData = false }: RequestOptions = {},
): Promise<T> {
  let res: Response
  try {
    res = await fetch(`/api/v1${path}`, {
      method,
      headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
      credentials: 'same-origin',
      signal,
    })
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') throw err
    throw new ApiError('network_error', 'No se pudo conectar con el servidor')
  }

  let envelope: ApiEnvelope<T> | null = null
  try {
    envelope = (await res.json()) as ApiEnvelope<T>
  } catch {
    // cuerpo no JSON: cae al error generico de abajo
  }

  if (res.status === 401 && (!envelope?.error || envelope.error.code === UNAUTHENTICATED)) {
    unauthorizedListeners.forEach((listener) => listener())
    throw new ApiError(UNAUTHENTICATED, 'Tu sesión expiró. Inicia sesión de nuevo.', 401)
  }
  if (res.status === 403) {
    throw new ApiError(envelope?.error?.code ?? 'forbidden', FORBIDDEN_MESSAGE, 403)
  }
  if (envelope?.error) {
    throw new ApiError(
      envelope.error.code,
      envelope.error.message,
      res.status,
      parseRetryAfter(res),
      envelope.error.details,
    )
  }
  if (!res.ok || (!noData && envelope?.data == null)) {
    throw new ApiError('bad_response', `Respuesta inesperada del servidor (${res.status})`, res.status)
  }
  return envelope?.data as T
}
