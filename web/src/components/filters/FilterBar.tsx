import { useState, type ReactNode } from 'react'
import { Button } from '@/components/ui/button'
import { BooleanFilter } from './BooleanFilter'
import { DateTimeFilter } from './DateTimeFilter'
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
/** "hosts" -> "host", "canales" -> "canal": basta pa las etiquetas de la app (todas regulares). */
function singular(label: string): string {
  return label.endsWith('les') ? label.slice(0, -2) : label.replace(/s$/, '')
}

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
  // "Más filtros": los numericos siempre, y los booleanos marcados `extra` (para no llenar la fila principal
  // de pastillas de poco uso, p. ej. "Agente desactualizado" o "Reinicio pendiente").
  const extras = fields.filter((f): f is NumericField | BooleanField => f.kind === 'numeric' || (f.kind === 'boolean' && f.extra === true))

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2">
        {fields.map((f) => {
          switch (f.kind) {
            case 'search':
              return <SearchBox key={`${f.key}-${resetKey}`} field={f} value={v[f.key] as string} onCommit={(q) => change({ [f.key]: q })} />
            case 'multi':
              return <MultiFilter key={f.key} field={f} selected={v[f.key] as string[]} onChange={(next) => change({ [f.key]: next })} />
            case 'boolean':
              return f.extra ? null : <BooleanFilter key={f.key} field={f} value={v[f.key] as boolean} onChange={(next) => change({ [f.key]: next })} />
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
        <ul className="flex flex-wrap items-center gap-2" aria-label="Filtros de más filtros activos">
          {extras.map((f) => {
            if (f.kind === 'boolean') {
              return v[f.key] ? <RemovableChip key={f.key} text={f.label} onRemove={() => change({ [f.key]: false })} /> : null
            }
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
      <div className="flex items-center gap-2 text-xs text-text-muted" aria-live="polite">
        {total !== null && (
          <span className="tabular-nums">
            {active && overall !== null ? `${total.toLocaleString('es-PE')} de ${overall.toLocaleString('es-PE')}` : total.toLocaleString('es-PE')}{' '}
            {total === 1 && !(active && overall !== null) ? singular(itemLabel) : itemLabel}
          </span>
        )}
      </div>
    </div>
  )
}
