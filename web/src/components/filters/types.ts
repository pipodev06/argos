// Campos de filtro declarativos: una misma lista alimenta la barra (UI) y useUrlFilters (URL).
// Sin dependencias de React ni de ninguna feature.

export interface FilterOption {
  id: string
  name: string
  /** Numero de elementos con esta opcion (se muestra junto al nombre). */
  count?: number
}

interface FieldBase {
  /** Clave en el objeto de filtros. */
  key: string
  /** Parametro de URL (por defecto = key). */
  param?: string
  label: string
}

export interface SearchField extends FieldBase {
  kind: 'search'
  ariaLabel: string
  placeholder: string
  maxLength?: number
}

/** Checkboxes (OR dentro del campo; el consumidor decide la semantica). Opciones estaticas (`options`, orden canonico y valores validos) o cargadas (`useOptions`). */
export interface MultiField extends FieldBase {
  kind: 'multi'
  /** Plural para el resumen "N <plural>". */
  plural: string
  options?: readonly FilterOption[]
  useOptions?: () => readonly FilterOption[]
  /** Muestra un buscador dentro de la pastilla si hay mas de N opciones. */
  searchAbove?: number
}

export interface NumericUnit {
  id: string
  label: string
  one: string
  many: string
  /** Multiplicador hacia la unidad de la API. */
  factor: number
}

/** Numerico con operador; vive dentro de "Mas filtros". El valor de la URL/API va en la unidad base. */
export interface NumericField extends FieldBase {
  kind: 'numeric'
  extra: true
  op: '>' | '<' | '≤'
  /** Tope valido (sin tope si se omite). */
  max?: number
  integer?: boolean
  /** Sufijo fijo ("%"), si no hay `units`. */
  suffix?: string
  units?: readonly NumericUnit[]
  defaultUnit?: string
  /** Texto de la unidad base cuando ninguna `unit` divide exacto ("s"). */
  baseUnit?: string
}

type ChoiceValue = string | number

/** Parametro de URL de valores fijos (orden, tamano de pagina); la barra lo ignora. */
interface ChoiceField extends FieldBase {
  kind: 'choice'
  values: readonly ChoiceValue[]
  /** Valor por defecto (se omite de la URL); puede depender de los campos ya leidos. */
  default: ChoiceValue | ((values: Record<string, unknown>) => ChoiceValue)
}

/** Instante (fecha y hora local elegida con el control nativo). El valor de la URL/API es RFC 3339 en UTC; vacio = sin filtro. */
export interface DateTimeField extends FieldBase {
  kind: 'datetime'
}

/** Chip on/off; en la URL `param=true` (ausente = apagado). */
export interface BooleanField extends FieldBase {
  kind: 'boolean'
}

export type FilterField = SearchField | MultiField | NumericField | ChoiceField | DateTimeField | BooleanField
export type FilterValue = string | number | boolean | string[] | null

type ValueOf<E> = E extends { kind: 'search' | 'datetime' }
  ? string
  : E extends { kind: 'boolean' }
    ? boolean
    : E extends { kind: 'multi' }
    ? string[]
    : E extends { kind: 'numeric' }
      ? number | null
      : E extends { kind: 'choice'; values: readonly (infer T)[] }
        ? T
        : never

/** Objeto de filtros derivado de la lista de campos (declarala `as const satisfies readonly FilterField[]`). */
export type FiltersOf<F extends readonly FilterField[]> = { [E in F[number] as E['key']]: ValueOf<E> }
