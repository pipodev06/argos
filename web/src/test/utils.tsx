import { render } from '@testing-library/react'
import type { ReactElement } from 'react'
import { MemoryRouter } from 'react-router'
import { vi } from 'vitest'
import { axe } from 'vitest-axe'

/** Render con Router (el proyecto no usa QueryClient: los datos vienen de `usePolledFetch` + `fetch`). */
export function renderWithRouter(ui: ReactElement, initialEntry = '/') {
  return render(<MemoryRouter initialEntries={[initialEntry]}>{ui}</MemoryRouter>)
}

/**
 * Axe sobre el documento completo (los diálogos de Radix van en un portal). jsdom no calcula estilos:
 * la regla `color-contrast` no puede evaluarse aquí, así que se desactiva (el contraste se fijó por token, ver CLAUDE.md).
 * `region` es de página completa (landmarks del AppShell): un componente aislado nunca la cumple.
 */
export function checkA11y(container: Element = document.body) {
  return axe(container, { rules: { 'color-contrast': { enabled: false }, region: { enabled: false } } })
}

export interface MockReply {
  status?: number
  data?: unknown
  error?: { code: string; message: string }
}

export interface RecordedCall {
  method: string
  path: string
  body: unknown
}

/**
 * Sustituye `fetch` por rutas "MÉTODO /ruta" (sin `/api/v1` ni query) con el envelope { data, error } del servidor.
 * Devuelve las llamadas hechas, en orden. Una ruta sin respuesta falla el test.
 */
export function mockApi(routes: Record<string, MockReply | (() => MockReply)>): RecordedCall[] {
  const calls: RecordedCall[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn((input: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      const path = input.replace(/^\/api\/v1/, '').split('?')[0]
      calls.push({ method, path, body: typeof init?.body === 'string' ? JSON.parse(init.body) : undefined })
      const entry = routes[`${method} ${path}`]
      if (!entry) return Promise.reject(new Error(`ruta sin mock: ${method} ${path}`))
      const r = typeof entry === 'function' ? entry() : entry
      const body = JSON.stringify({ data: r.data ?? null, error: r.error ?? null })
      return Promise.resolve(new Response(body, { status: r.status ?? 200, headers: { 'Content-Type': 'application/json' } }))
    }),
  )
  return calls
}
