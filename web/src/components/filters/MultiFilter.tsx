import { useState } from 'react'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { FilterPill } from './FilterPill'
import { CHECK_ITEM } from './styles'
import type { FilterOption, MultiField } from './types'

const NO_OPTIONS: readonly FilterOption[] = []
const useNoOptions = () => NO_OPTIONS

interface MultiFilterProps {
  field: MultiField
  selected: string[]
  onChange: (next: string[]) => void
}

/** Pastilla de multiselección con checkboxes (OR; la semántica AND la aplica el consumidor). Resumen: el nombre si hay 1, "N <plural>" si hay más. */
export function MultiFilter({ field, selected, onChange }: MultiFilterProps) {
  const useOptions = field.useOptions ?? useNoOptions
  const loaded = useOptions()
  const options = field.options ?? loaded
  const [query, setQuery] = useState('')
  const searchable = field.searchAbove !== undefined && options.length > field.searchAbove
  const q = query.trim().toLowerCase()
  const visible = searchable && q ? options.filter((o) => o.name.toLowerCase().includes(q)) : options
  const summary =
    selected.length === 0
      ? null
      : selected.length === 1
        ? (options.find((o) => o.id === selected[0])?.name ?? selected[0])
        : `${selected.length} ${field.plural}`

  function toggle(id: string, on: boolean) {
    // Opciones estáticas: orden canónico; cargadas: en orden de selección.
    if (field.options) onChange(field.options.map((o) => o.id).filter((x) => (x === id ? on : selected.includes(x))))
    else onChange(on ? [...selected, id] : selected.filter((x) => x !== id))
  }

  return (
    <FilterPill
      label={field.label}
      summary={summary}
      onClear={() => onChange([])}
      onOpenChange={() => setQuery('')}
      contentClassName={field.contentClassName}
    >
      {() => (
        <>
          {searchable && (
            <Input
              aria-label={`Buscar ${field.label.toLowerCase()}`}
              placeholder="Buscar"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              className="mb-1 h-8"
              autoComplete="off"
            />
          )}
          {visible.map((o) => (
            <label key={o.id} className={CHECK_ITEM}>
              <Checkbox checked={selected.includes(o.id)} onCheckedChange={(v) => toggle(o.id, v === true)} />
              <span className="min-w-0 flex-1 truncate">{o.name}</span>
              {o.count !== undefined && <span className="text-xs tabular-nums text-text-muted">{o.count}</span>}
            </label>
          ))}
          {searchable && visible.length === 0 && <p className="px-2 py-1 text-text-muted">Sin resultados</p>}
        </>
      )}
    </FilterPill>
  )
}
