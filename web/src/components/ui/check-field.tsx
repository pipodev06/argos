import { Checkbox } from '@/components/ui/checkbox'

interface CheckFieldProps {
  id: string
  label: string
  hint?: string
  checked: boolean
  onChange: (v: boolean) => void
}

/** Casilla con etiqueta visible y ayuda opcional (patrón único de casillas de formulario). */
export function CheckField({ id, label, hint, checked, onChange }: CheckFieldProps) {
  return (
    <label htmlFor={id} className="flex items-start gap-2 text-sm">
      <Checkbox id={id} className="mt-0.5" checked={checked} onCheckedChange={(v) => onChange(v === true)} />
      <span>
        {label}
        {hint && <span className="block text-xs text-text-muted">{hint}</span>}
      </span>
    </label>
  )
}
