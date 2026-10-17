import { useState } from "react"
import { CalendarIcon } from "lucide-react"
import { cn } from "cn"
import { es } from "react-day-picker/locale"
import { Button } from "@/components/ui/button"
import { Calendar } from "@/components/ui/calendar"
import { InlineError } from "@/components/ui/inline-error"
import { Input } from "@/components/ui/input"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"

const pad = (n: number) => String(n).padStart(2, "0")

/** "YYYY-MM-DD" o "YYYY-MM-DDTHH:mm" (hora local) a Date; '' o invalido = undefined. */
function parseLocal(value: string): Date | undefined {
  const m = /^(\d{4})-(\d{2})-(\d{2})(?:T(\d{2}):(\d{2}))?$/.exec(value)
  if (!m) return undefined
  const d = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]), Number(m[4] ?? 0), Number(m[5] ?? 0))
  return Number.isNaN(d.getTime()) ? undefined : d
}

const dateText = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
const timeText = (d: Date) => `${pad(d.getHours())}:${pad(d.getMinutes())}`

const LABEL = new Intl.DateTimeFormat("es-PE", { day: "2-digit", month: "short", year: "numeric" })

interface DateTimePickerProps {
  id?: string
  /** Hora local: "YYYY-MM-DDTHH:mm" (o "YYYY-MM-DD" sin hora); '' = vacio. Mismo formato que el input nativo. */
  value: string
  onChange: (next: string) => void
  /** false = solo fecha. */
  withTime?: boolean
  placeholder?: string
  disabled?: boolean
  className?: string
  "aria-label"?: string
  "aria-invalid"?: boolean
  "aria-describedby"?: string
}

/** Selector unico de fecha (y hora) de la app: Popover + Calendar de shadcn, nunca el selector nativo del navegador. */
function DateTimePicker({
  id,
  value,
  onChange,
  withTime = true,
  placeholder = "Elegir fecha",
  disabled,
  className,
  ...aria
}: DateTimePickerProps) {
  const [open, setOpen] = useState(false)
  const selected = parseLocal(value)
  const time = selected ? timeText(selected) : "00:00"

  function emit(day: Date, hhmm: string) {
    onChange(withTime ? `${dateText(day)}T${hhmm}` : dateText(day))
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          id={id}
          type="button"
          variant="outline"
          disabled={disabled}
          className={cn("w-full justify-start font-normal", !selected && "text-text-muted", className)}
          {...aria}
        >
          <CalendarIcon strokeWidth={1.5} aria-hidden />
          <span className="truncate tabular-nums">
            {selected ? `${LABEL.format(selected)}${withTime ? `, ${time}` : ""}` : placeholder}
          </span>
        </Button>
      </PopoverTrigger>
      {/* Solo se cierra con clic fuera, Escape o "Listo": si la página se re-renderiza al elegir (p. ej. carga el
          informe) y el foco cae al body, Radix lo tomaría como "foco fuera" y cerraría antes de ajustar la hora. */}
      <PopoverContent className="w-auto p-0" align="start" onFocusOutside={(e) => e.preventDefault()}>
        <Calendar
          mode="single"
          locale={es}
          selected={selected}
          defaultMonth={selected}
          onSelect={(day) => {
            if (!day) return
            emit(day, time)
            if (!withTime) setOpen(false)
          }}
        />
        <div className="flex items-center gap-2 border-t border-border p-2">
          {withTime && (
            <Input
              type="time"
              aria-label="Hora"
              value={time}
              disabled={!selected}
              onChange={(e) => selected && e.target.value && emit(selected, e.target.value)}
              className="h-8 w-28 appearance-none tabular-nums [&::-webkit-calendar-picker-indicator]:hidden"
            />
          )}
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="ml-auto"
            disabled={!selected}
            onClick={() => {
              onChange("")
              setOpen(false)
            }}
          >
            Limpiar
          </Button>
          {withTime && (
            <Button type="button" size="sm" onClick={() => setOpen(false)}>
              Listo
            </Button>
          )}
        </div>
      </PopoverContent>
    </Popover>
  )
}

interface DateTimeFieldProps extends Omit<DateTimePickerProps, "id" | "aria-invalid" | "aria-describedby"> {
  id: string
  label: string
  error?: string | null
}

/** `DateTimePicker` con label visible y error, con la misma forma que `FormField`. */
function DateTimeField({ id, label, error, ...picker }: DateTimeFieldProps) {
  const errorId = error ? `${id}-error` : undefined
  return (
    <div className="grid gap-1.5">
      <label htmlFor={id} className="text-sm font-medium text-text">
        {label}
      </label>
      <DateTimePicker id={id} aria-invalid={error ? true : undefined} aria-describedby={errorId} {...picker} />
      {error && <InlineError id={errorId}>{error}</InlineError>}
    </div>
  )
}

export { DateTimeField, DateTimePicker }
