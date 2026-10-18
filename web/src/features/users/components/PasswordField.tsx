import { FormField } from '@/components/ui/form-field'
import { MIN_PASSWORD_LENGTH } from '@/features/auth/constants'
import { passwordLength } from '../utils/userQuery'

interface PasswordFieldProps {
  id: string
  label: string
  value: string
  onChange: (value: string) => void
  error?: string | null
}

/** Campo de contraseña inicial/restablecida con indicador de largo (15-128, sin reglas de composición). */
export function PasswordField({ id, label, value, onChange, error }: PasswordFieldProps) {
  const n = passwordLength(value)
  return (
    <FormField
      id={id}
      label={label}
      type="password"
      value={value}
      onChange={(e) => onChange(e.target.value)}
      autoComplete="new-password"
      hint={`${n} caracteres (mínimo ${MIN_PASSWORD_LENGTH})`}
      error={error}
    />
  )
}
