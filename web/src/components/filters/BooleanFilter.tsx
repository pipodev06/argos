import { CheckIcon } from 'lucide-react'
import { PILL } from './FilterPill'
import type { BooleanField } from './types'

interface BooleanFilterProps {
  field: BooleanField
  value: boolean
  onChange: (next: boolean) => void
}

/** Chip on/off: encendido = fondo `muted`, texto semibold e icono de marca de verificación (no solo color). */
export function BooleanFilter({ field, value, onChange }: BooleanFilterProps) {
  return (
    <button
      type="button"
      aria-pressed={value}
      title={field.label}
      onClick={() => onChange(!value)}
      className={`${PILL} ${value ? 'border-solid bg-muted font-medium' : ''}`}
    >
      {value && <CheckIcon className="size-4 shrink-0" strokeWidth={1.5} aria-hidden />}
      {field.label}
    </button>
  )
}
