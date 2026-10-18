import { CheckField } from '@/components/ui/check-field'
import { FormField } from '@/components/ui/form-field'

interface RetentionFieldProps {
  id: string
  value: string
  inherit: boolean
  error: string | null
  onValue: (v: string) => void
  onInherit: (v: boolean) => void
}

/** Campo "Retención de detalle (días)" con casilla "Heredar" (grupos y hosts). */
export function RetentionField({ id, value, inherit, error, onValue, onInherit }: RetentionFieldProps) {
  return (
    <div className="grid gap-2">
      <FormField
        id={id}
        label="Retención de detalle (días)"
        inputMode="numeric"
        className="w-28"
        value={value}
        onChange={(e) => onValue(e.target.value)}
        disabled={inherit}
        autoComplete="off"
        error={error}
      />
      <CheckField id={`${id}-inherit`} label="Heredar" checked={inherit} onChange={onInherit} />
    </div>
  )
}
