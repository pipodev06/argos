import { useState, type ReactNode } from 'react'
import { Button } from '@/components/ui/button'
import { DateTimeFilter } from './DateTimeFilter'
import { RemovableChip } from './FilterPill'
import { MoreFilters } from './MoreFilters'
import { MultiFilter } from './MultiFilter'
import { numericChipText } from './numeric'
import { SearchBox } from './SearchBox'
import type { FilterField, FilterValue, FiltersOf, NumericField } from './types'
import { hasActiveFilters } from './urlState'

interface FilterBarProps<F extends readonly FilterField[]> {
  fields: F
  values: FiltersOf<F>
  onChange: (patch: Partial<FiltersOf<F>>) => void
  onClear: () => void
  /** Sustantivo del contador ("hosts"). */
  itemLabel: string
  /** Total con filtros; null = aún no se sabe. */
  total: number | null
  /** Total sin filtros (para "37 de 102"); null = aún no se sabe. */
  overall: number | null
  /** Controles extra al final de la fila (p. ej. "Columnas"). */
  actions?: ReactNode
}

type Values = Record<string, FilterValue>

/**
 * Barra de filtros dirigida por configuración: [Buscar][pastillas…][Más filtros][acciones] +
 * fila de filtros numéricos activos con "Limpiar" + contador (aria-live).
 */
export function FilterBar<F extends readonly FilterField[]>({
  fields,
  values,
  onChange,
  onClear,
  itemLabel,
  total,
  overall,
  actions,
}: FilterBarProps<F>) {
  const [resetKey, setResetKey] = useState(0)
  // Los tipos por campo se resuelven en runtime por `kind`; aquí se trabaja con la forma suelta.
  const v = values as Values
  const change = onChange as (patch: Values) => void
  const active = hasActiveFilters(fields, values)
  const extras = fields.filter((f): f is NumericField => f.kind === 'numeric')

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2">
        {fields.map((f) => {
          switch (f.kind) {
            case 'search':
              return <SearchBox key={`${f.key}-${resetKey}`} field={f} value={v[f.key] as string} onCommit={(q) => change({ [f.key]: q })} />
            case 'multi':
              return <MultiFilter key={f.key} field={f} selected={v[f.key] as string[]} onChange={(next) => change({ [f.key]: next })} />
            case 'datetime':
              return <DateTimeFilter key={f.key} field={f} value={v[f.key] as string} onChange={(next) => change({ [f.key]: next })} />
            default:
              return null
          }
        })}
        <MoreFilters fields={extras} onApply={(key, value) => change({ [key]: value })} />
        {actions}
      </div>
      {active && (
        <ul className="flex flex-wrap items-center gap-2" aria-label="Filtros numéricos activos">
          {extras.map((f) => {
            const value = v[f.key] as number | null
            return value === null ? null : (
              <RemovableChip key={f.key} text={numericChipText(f, value)} onRemove={() => change({ [f.key]: null })} />
            )
          })}
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
            </Button>
          </li>
        </ul>
      )}
      <div className="flex items-center gap-3 text-xs text-text-muted" aria-live="polite">
        {total !== null && (
          <span className="tabular-nums">
            {active && overall !== null ? `${total.toLocaleString('es-PE')} de ${overall.toLocaleString('es-PE')}` : total.toLocaleString('es-PE')}{' '}
            {itemLabel}
          </span>
        )}
      </div>
    </div>
  )
}
