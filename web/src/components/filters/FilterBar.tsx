import { useState, type ReactNode } from 'react'
import { XIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { DateRangeFilter } from './DateRangeFilter'
import { RemovableChip } from './FilterPill'
import { MoreFilters } from './MoreFilters'
import { MultiFilter } from './MultiFilter'
import { numericChipText } from './numeric'
import { SearchBox } from './SearchBox'
import type { BooleanField, FilterField, FilterValue, FiltersOf, NumericField } from './types'
import { hasActiveFilters } from './urlState'

interface FilterBarProps<F extends readonly FilterField[]> {
  fields: F
  values: FiltersOf<F>
  onChange: (patch: Partial<FiltersOf<F>>) => void
  onClear: () => void
  /** Controles extra al final de la fila, alineados a la derecha (p. ej. "Columnas"). */
  actions?: ReactNode
}

type Values = Record<string, FilterValue>

/**
 * Barra de filtros dirigida por configuración, en una fila (patrón de la tabla de shadcn):
 * [Buscar, crece][+ filtros…][Más filtros][activos][Limpiar][acciones].
 * Sin contador: el total ya lo dice el pie de la tabla ("1–15 de 397").
 */
export function FilterBar<F extends readonly FilterField[]>({ fields, values, onChange, onClear, actions }: FilterBarProps<F>) {
  const [resetKey, setResetKey] = useState(0)
  // Los tipos por campo se resuelven en runtime por `kind`; aquí se trabaja con la forma suelta.
  const v = values as Values
  const change = onChange as (patch: Values) => void
  const active = hasActiveFilters(fields, values)
  // "Más filtros": numéricos y booleanos (de poco uso; no ocupan la fila principal).
  const extras = fields.filter((f): f is NumericField | BooleanField => f.kind === 'numeric' || f.kind === 'boolean')

  return (
    <ul className="flex flex-wrap items-center gap-2" aria-label="Filtros">
      {fields.map((f) => {
        switch (f.kind) {
          case 'search':
            return (
              <li key={`${f.key}-${resetKey}`} className="min-w-48 flex-1">
                <SearchBox field={f} value={v[f.key] as string} onCommit={(q) => change({ [f.key]: q })} />
              </li>
            )
          case 'multi':
            return (
              <li key={f.key}>
                <MultiFilter field={f} selected={v[f.key] as string[]} onChange={(next) => change({ [f.key]: next })} />
              </li>
            )
          case 'datetime': {
            // Un campo "hasta" se pinta dentro del filtro de rango de su "desde".
            if (!f.rangeEnd) return null
            const end = f.rangeEnd
            return (
              <li key={f.key}>
                <DateRangeFilter
                  label={f.label}
                  from={v[f.key] as string}
                  to={v[end] as string}
                  onChange={(from, to) => change({ [f.key]: from, [end]: to })}
                />
              </li>
            )
          }
          default:
            return null
        }
      })}
      {extras.length > 0 && (
        <li>
          <MoreFilters fields={extras} onApply={(key, value) => change({ [key]: value })} />
        </li>
      )}
      {extras.map((f) => {
        if (f.kind === 'boolean') {
          return v[f.key] ? <RemovableChip key={f.key} text={f.label} onRemove={() => change({ [f.key]: false })} /> : null
        }
        const value = v[f.key] as number | null
        return value === null ? null : <RemovableChip key={f.key} text={numericChipText(f, value)} onRemove={() => change({ [f.key]: null })} />
      })}
      {active && (
        <li>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              setResetKey((k) => k + 1)
              onClear()
            }}
          >
            Limpiar
            <XIcon strokeWidth={1.5} aria-hidden />
          </Button>
        </li>
      )}
      {actions && <li className="flex items-center gap-2">{actions}</li>}
    </ul>
  )
}
