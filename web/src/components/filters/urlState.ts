// Filtros <-> URL. Funciones puras: sin React ni router.
import type { FilterField, FilterValue, FiltersOf } from './types'

type Values = Record<string, FilterValue>

export const paramOf = (f: FilterField): string => f.param ?? f.key

function unique(list: string[]): string[] {
  return [...new Set(list)]
}

function choiceDefault(f: Extract<FilterField, { kind: 'choice' }>, values: Record<string, unknown>) {
  return typeof f.default === 'function' ? f.default(values) : f.default
}

/** Lee la URL; ignora valores invalidos (fuera de rango, no permitidos, mal formados). */
export function parseFilters<F extends readonly FilterField[]>(fields: F, params: URLSearchParams): FiltersOf<F> {
  const out: Values = {}
  for (const f of fields) {
    const p = paramOf(f)
    switch (f.kind) {
      case 'search':
        out[f.key] = params.get(p) ?? ''
        break
      case 'datetime': {
        const raw = params.get(p) ?? ''
        out[f.key] = Number.isNaN(Date.parse(raw)) ? '' : raw
        break
      }
      case 'multi': {
        const allowed = f.options?.map((o) => o.id)
        out[f.key] = unique(params.getAll(p).filter((v) => (allowed ? allowed.includes(v) : v !== '')))
        break
      }
      case 'numeric': {
        const raw = params.get(p)
        const n = raw ? Number(raw) : NaN
        const bad = !Number.isFinite(n) || n < 0 || (f.max !== undefined && n > f.max)
        out[f.key] = bad ? null : f.integer ? Math.floor(n) : n
        break
      }
      case 'choice': {
        const raw = params.get(p)
        const hit = f.values.find((v) => (typeof v === 'number' ? Number(raw) === v : raw === v))
        out[f.key] = hit ?? choiceDefault(f, out)
        break
      }
    }
  }
  return out as FiltersOf<F>
}

/** Serializa a la URL (en el orden de los campos) omitiendo lo vacio y los valores por defecto. */
export function serializeFilters<F extends readonly FilterField[]>(fields: F, filters: FiltersOf<F>): URLSearchParams {
  const v = filters as Values
  const p = new URLSearchParams()
  for (const f of fields) {
    const value = v[f.key]
    const name = paramOf(f)
    switch (f.kind) {
      case 'search':
      case 'datetime':
        if (value) p.set(name, String(value))
        break
      case 'multi':
        for (const x of value as string[]) p.append(name, x)
        break
      case 'numeric':
        if (value !== null) p.set(name, String(value))
        break
      case 'choice':
        if (value !== choiceDefault(f, v)) p.set(name, String(value))
        break
    }
  }
  return p
}

/** Hay algun filtro (busqueda, multi o numerico) distinto de vacio; los `choice` no cuentan. */
export function hasActiveFilters<F extends readonly FilterField[]>(fields: F, filters: FiltersOf<F>): boolean {
  const v = filters as Values
  return fields.some((f) => {
    const value = v[f.key]
    if (f.kind === 'choice') return false
    return Array.isArray(value) ? value.length > 0 : value !== null && value !== ''
  })
}

/** Vacia todo filtro; conserva los `choice` (orden, tamano de pagina). */
export function clearedFilters<F extends readonly FilterField[]>(fields: F, filters: FiltersOf<F>): FiltersOf<F> {
  const out: Values = { ...(filters as Values) }
  for (const f of fields) {
    if (f.kind === 'search' || f.kind === 'datetime') out[f.key] = ''
    else if (f.kind === 'multi') out[f.key] = []
    else if (f.kind === 'numeric') out[f.key] = null
  }
  return out as FiltersOf<F>
}
