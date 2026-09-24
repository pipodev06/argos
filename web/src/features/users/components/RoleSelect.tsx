import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import type { Role } from '@/features/auth/types/auth.types'
import { roleLabel } from '@/features/auth/utils/format'

const ROLES: readonly Role[] = ['viewer', 'admin']

interface RoleSelectProps {
  id: string
  label: string
  value: Role
  onChange: (role: Role) => void
}

export function RoleSelect({ id, label, value, onChange }: RoleSelectProps) {
  return (
    <div className="grid gap-1.5">
      <label htmlFor={id} className="text-sm font-medium text-text">
        {label}
      </label>
      <Select value={value} onValueChange={(v) => onChange(v as Role)}>
        <SelectTrigger id={id} className="w-full">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {ROLES.map((r) => (
            <SelectItem key={r} value={r}>
              {roleLabel(r)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  )
}
