import { SelectField } from '@/components/ui/select-field'
import { useServiceChecks } from '../hooks/useChecks'
import { CHECK_TYPE_LABELS } from '../utils/format'

interface CheckSelectFieldProps {
  id: string
  value: string
  onChange: (checkId: string) => void
  /** Nombre ya guardado (al editar), por si el chequeo no figura en la lista. */
  currentName?: string | null
  disabled?: boolean
  error?: string | null
}

/** Selector del chequeo que vigila una regla. Montar solo donde se usa (consulta la API). */
export function CheckSelectField({ id, value, onChange, currentName, disabled, error }: CheckSelectFieldProps) {
  const { data } = useServiceChecks()
  const options = (data ?? []).map((c) => ({ value: c.id, label: `${c.name} (${CHECK_TYPE_LABELS[c.type]})` }))
  if (value && currentName && !options.some((o) => o.value === value)) options.unshift({ value, label: currentName })
  return (
    <SelectField
      id={id}
      label="Chequeo"
      value={value}
      onChange={onChange}
      options={options}
      placeholder={data ? (options.length === 0 ? 'No hay chequeos' : 'Elige un chequeo') : 'Cargando…'}
      disabled={disabled}
      error={error}
    />
  )
}
