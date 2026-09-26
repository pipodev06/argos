import { IconCheck } from '@tabler/icons-react'
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
      className={`${PILL} gap-1 px-3 text-left hover:bg-muted ${value ? 'bg-muted' : ''}`}
    >
      <span className={`min-w-0 flex-1 truncate ${value ? 'font-medium' : ''}`}>{field.label}</span>
      {value && <IconCheck className="size-4 shrink-0" stroke={1.5} aria-hidden />}
    </button>
  )
}
