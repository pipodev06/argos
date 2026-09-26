import { CheckField } from '@/components/ui/check-field'
import { FormField } from '@/components/ui/form-field'
import { MAX_DETAIL_DAYS, MIN_DETAIL_DAYS } from '../utils/retention'

interface RetentionFieldProps {
  id: string
  value: string
  inherit: boolean
  error: string | null
  /** Texto de lo que se hereda, p. ej. "el global (90 días)". */
  inheritsFrom: string
  onValue: (v: string) => void
  onInherit: (v: boolean) => void
}

/** Campo "Retención de detalle (días)" con casilla "Heredar" (grupos y hosts). */
export function RetentionField({ id, value, inherit, error, inheritsFrom, onValue, onInherit }: RetentionFieldProps) {
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
        hint={`Entre ${MIN_DETAIL_DAYS} y ${MAX_DETAIL_DAYS} días de detalle de 30 s; después solo queda el resumen por hora.`}
        error={error}
      />
      <CheckField id={`${id}-inherit`} label="Heredar" hint={`Usa ${inheritsFrom}.`} checked={inherit} onChange={onInherit} />
    </div>
  )
}
