import { useId, useState } from 'react'
import { Button } from '@/components/ui/button'
import { DateTimePicker } from '@/components/ui/date-time-picker'
import { formatDateTimeSummary, fromLocalInput, toLocalInput } from './datetime'
import { FilterPill } from './FilterPill'
import type { DateTimeField } from './types'

interface DateTimeFilterProps {
  field: DateTimeField
  /** RFC 3339 o ''. */
  value: string
  onChange: (next: string) => void
}

/** Pastilla con selector de fecha y hora (hora local); aplica con "Aplicar". */
export function DateTimeFilter({ field, value, onChange }: DateTimeFilterProps) {
  const id = useId()
  const [draft, setDraft] = useState('')
  const summary = value ? formatDateTimeSummary(value) : null

  return (
    <FilterPill
      label={field.label}
      summary={summary}
      onClear={() => onChange('')}
      onOpenChange={(open) => open && setDraft(toLocalInput(value))}
      contentClassName="w-64"
    >
      {(close) => (
        <div className="flex flex-col gap-2 p-1">
          <label htmlFor={id} className="text-xs text-text-muted">
            {field.label} (hora local)
          </label>
          <DateTimePicker id={id} value={draft} onChange={setDraft} className="h-8" />
          <Button
            size="sm"
            disabled={!draft}
            onClick={() => {
              onChange(fromLocalInput(draft))
              close()
            }}
          >
            Aplicar
          </Button>
        </div>
      )}
    </FilterPill>
  )
}
