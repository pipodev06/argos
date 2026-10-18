import { es } from "react-day-picker/locale"
import { Calendar } from "@/components/ui/calendar"

/** Calendario de un dia en español; se carga aparte (lazy) desde `DateTimePicker`. */
export function DateCalendar({ selected, maxDate, onSelect }: { selected?: Date; maxDate?: Date; onSelect: (day: Date) => void }) {
  return (
    <Calendar
      mode="single"
      locale={es}
      selected={selected}
      defaultMonth={selected}
      disabled={maxDate ? { after: maxDate } : undefined}
      endMonth={maxDate}
      onSelect={(day) => {
        if (day) onSelect(day)
      }}
    />
  )
}
