import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError, isUnauthenticated } from '@/lib/api'

interface Options {
  /** Milisegundos entre refrescos. */
  intervalMs: number
  /** Debe ser estable (constante de modulo). Errores tras los cuales no tiene sentido seguir consultando (ademas del 401, que siempre detiene). */
  stopOn?: (err: unknown) => boolean
  /** Sin refrescos automaticos (ni al volver a la pestana); solo la carga inicial y `refresh()`. */
  paused?: boolean
}

interface Result<T> {
  source: unknown
  data: T
  updatedAt: number
}

interface Failure {
  source: unknown
  message: string
  /** Status HTTP si el error vino del servidor (ApiError), 0 si no. */
  status: number
}

/** Errores definitivos de cliente (400/404): repetir la misma consulta no los arregla. */
export function isClientError(err: unknown): boolean {
  return err instanceof ApiError && (err.status === 400 || err.status === 404)
}

/** Intervalo ±15 % aleatorio. */
function jittered(ms: number): number {
  return Math.round(ms * (0.85 + Math.random() * 0.3))
}

/**
 * Consulta periodica con AbortController. `fetcher` debe ser estable (useCallback): cuando cambia
 * (otro host o rango) se descartan los datos anteriores y se vuelve a cargar. Se pausa mientras la
 * pestana esta oculta (y refresca al volver), con jitter de ±15 % en el intervalo y se detiene ante 401 o `stopOn`.
 */
export function usePolledFetch<T>(
  fetcher: (signal: AbortSignal) => Promise<T>,
  { intervalMs, stopOn, paused = false }: Options,
) {
  const [result, setResult] = useState<Result<T> | null>(null)
  const [failure, setFailure] = useState<Failure | null>(null)
  const runRef = useRef<() => Promise<void>>(() => Promise.resolve())

  useEffect(() => {
    let controller: AbortController | null = null
    let timer: ReturnType<typeof setTimeout> | null = null

    const stop = () => {
      if (timer !== null) clearTimeout(timer)
      timer = null
      document.removeEventListener('visibilitychange', onVisibility)
    }

    const run = async () => {
      controller?.abort()
      const current = new AbortController()
      controller = current
      try {
        const data = await fetcher(current.signal)
        if (current.signal.aborted) return
        setResult({ source: fetcher, data, updatedAt: Date.now() })
        setFailure(null)
      } catch (err) {
        if (current.signal.aborted) return
        // 401: el manejo central (lib/api) ya llevo al login; solo se detiene el polling.
        if (isUnauthenticated(err)) {
          stop()
          return
        }
        setFailure({
          source: fetcher,
          message: err instanceof Error ? err.message : 'Error desconocido',
          status: err instanceof ApiError ? err.status : 0,
        })
        if (stopOn?.(err)) stop()
      }
    }

    // Intervalo con jitter para que varias pantallas no consulten en el mismo instante.
    function schedule() {
      timer = setTimeout(() => {
        if (!document.hidden) void run()
        schedule()
      }, jittered(intervalMs))
    }

    function onVisibility() {
      if (!document.hidden) void run()
    }

    runRef.current = run
    // fetch inicial: el setState ocurre tras await, no de forma sincrona (falso positivo del lint)
    // oxlint-disable-next-line react/set-state-in-effect
    void run()
    if (!paused) {
      schedule()
      document.addEventListener('visibilitychange', onVisibility)
    }

    return () => {
      stop()
      controller?.abort()
    }
  }, [fetcher, intervalMs, stopOn, paused])

  const refresh = useCallback(() => runRef.current(), [])

  // Los datos/errores de un `fetcher` anterior (otro host/rango) no se muestran.
  const data = result?.source === fetcher ? result.data : null
  const error = failure?.source === fetcher ? failure.message : null
  return {
    data,
    error,
    errorStatus: failure?.source === fetcher ? failure.status : null,
    updatedAt: result?.source === fetcher ? result.updatedAt : null,
    loading: data === null && error === null,
    refresh,
  }
}
