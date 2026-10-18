import { InlineError } from "@/components/ui/inline-error"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"

interface SelectFieldProps {
  id: string
  label: string
  value: string
  onChange: (value: string) => void
  options: readonly { value: string; label: string }[]
  placeholder?: string
  hint?: string
  error?: string | null
  disabled?: boolean
}

// Campo de lista de formularios: label visible + select + ayuda + error (mismo patron que FormField).
function SelectField({ id, label, value, onChange, options, placeholder, hint, error, disabled }: SelectFieldProps) {
  const hintId = hint ? `${id}-hint` : undefined
  const errorId = error ? `${id}-error` : undefined
  return (
    <div className="grid gap-1">
      <label htmlFor={id} className="text-sm font-medium text-text">
        {label}
      </label>
      <Select value={value} onValueChange={onChange} disabled={disabled}>
        <SelectTrigger
          id={id}
          className="w-full"
          aria-invalid={error ? true : undefined}
          aria-describedby={[hintId, errorId].filter(Boolean).join(" ") || undefined}
        >
          <SelectValue placeholder={placeholder} />
        </SelectTrigger>
        <SelectContent>
          {options.map((o) => (
            <SelectItem key={o.value} value={o.value}>
              {o.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {hint && (
        <p id={hintId} className="text-xs text-text-muted">
          {hint}
        </p>
      )}
      {error && <InlineError id={errorId}>{error}</InlineError>}
    </div>
  )
}

export { SelectField }
