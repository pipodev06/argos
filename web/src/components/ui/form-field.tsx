import * as React from "react"
import { InlineError } from "@/components/ui/inline-error"
import { Input } from "@/components/ui/input"

interface FormFieldProps extends Omit<React.ComponentProps<"input">, "id"> {
  id: string
  label: string
  hint?: string
  error?: string | null
}

// Campo unico de formularios: label visible + input + ayuda + error (aria-describedby / aria-invalid).
function FormField({ id, label, hint, error, ...inputProps }: FormFieldProps) {
  const hintId = hint ? `${id}-hint` : undefined
  const errorId = error ? `${id}-error` : undefined
  return (
    <div className="grid gap-1.5">
      <label htmlFor={id} className="text-sm font-medium text-text">
        {label}
      </label>
      <Input
        id={id}
        name={id}
        aria-invalid={error ? true : undefined}
        aria-describedby={[hintId, errorId].filter(Boolean).join(" ") || undefined}
        {...inputProps}
      />
      {hint && (
        <p id={hintId} className="text-xs text-text-muted">
          {hint}
        </p>
      )}
      {error && <InlineError id={errorId}>{error}</InlineError>}
    </div>
  )
}

export { FormField }
