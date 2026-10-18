import { useId, useState } from 'react'
import { useNow } from '@/hooks/useNow'
import { Button } from '@/components/ui/button'
import { DateTimePicker } from '@/components/ui/date-time-picker'
import { formatDateTimeSummary, fromLocalInput, toLocalInput } from './datetime'
import { FilterPill } from './FilterPill'
import { ITEM } from './styles'

const HOUR = 3600_000

/** Atajos: "desde" relativo a ahora, sin "hasta" (= hasta el momento de la consulta). */
const PRESETS: readonly { label: string; from: (now: Date) => Date }[] = [
  { label: 'Hoy', from: (now) => new Date(now.getFullYear(), now.getMonth(), now.getDate()) },
  { label: 'Últimas 24 h', from: (now) => new Date(now.getTime() - 24 * HOUR) },
  { label: 'Últimos 7 días', from: (now) => new Date(now.getTime() - 7 * 24 * HOUR) },
  { label: 'Últimos 30 días', from: (now) => new Date(now.getTime() - 30 * 24 * HOUR) },
]

const short = (iso: string) => formatDateTimeSummary(iso).replace(/\/\d{4}/, '')

function summary(from: string, to: string): string | null {
  if (from && to) return `${short(from)} – ${short(to)}`
  if (from) return `desde ${short(from)}`
  if (to) return `hasta ${short(to)}`
  return null
}

interface DateRangeFilterProps {
  label: string
  /** RFC 3339 o ''. */
  from: string
  to: string
  onChange: (from: string, to: string) => void
}

/** Un solo filtro de fecha (Desde + Hasta) con atajos; reemplaza a dos pastillas sueltas. */
export function DateRangeFilter({ label, from, to, onChange }: DateRangeFilterProps) {
  const id = useId()
  const [draftFrom, setDraftFrom] = useState('')
  const [draftTo, setDraftTo] = useState('')
  const today = new Date(useNow(60_000))

  return (
    <FilterPill
      label={label}
      summary={summary(from, to)}
      onClear={() => onChange('', '')}
      onOpenChange={(open) => {
        if (!open) return
        setDraftFrom(from ? toLocalInput(from) : '')
        setDraftTo(to ? toLocalInput(to) : '')
      }}
      contentClassName="w-72"
    >
      {(close) => (
        <div className="flex flex-col gap-1">
          {PRESETS.map((p) => (
            <button
              key={p.label}
              type="button"
              className={ITEM}
              onClick={() => {
                onChange(fromLocalInput(toLocalInput(p.from(new Date()).toISOString())), '')
                close()
              }}
            >
              {p.label}
            </button>
          ))}
          <div aria-hidden className="-mx-1 my-1 h-px bg-border" />
          <p id={id} className="px-2 text-xs text-text-muted">
            Personalizado
          </p>
          <div role="group" aria-labelledby={id} className="grid gap-2 px-1 pb-1">
            <DateTimePicker aria-label="Desde" placeholder="Desde" value={draftFrom} onChange={setDraftFrom} maxDate={today} className="h-8" />
            <DateTimePicker aria-label="Hasta" placeholder="Hasta" value={draftTo} onChange={setDraftTo} maxDate={today} className="h-8" />
            <Button
              size="sm"
              disabled={!draftFrom && !draftTo}
              onClick={() => {
                onChange(draftFrom ? fromLocalInput(draftFrom) : '', draftTo ? fromLocalInput(draftTo) : '')
                close()
              }}
            >
              Aplicar
            </Button>
          </div>
        </div>
      )}
    </FilterPill>
  )
}
