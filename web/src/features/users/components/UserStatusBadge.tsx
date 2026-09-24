import { IconCircleCheck, IconCircleOff } from '@tabler/icons-react'

export function UserStatusBadge({ disabled }: { disabled: boolean }) {
  const Icon = disabled ? IconCircleOff : IconCircleCheck
  return (
    <span
      className={`inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-xs font-medium text-text ${disabled ? 'bg-text-muted/10' : 'bg-ok/10'}`}
    >
      <Icon className={`size-3.5 shrink-0 ${disabled ? 'text-text-muted' : 'text-ok'}`} stroke={1.5} aria-hidden />
      {disabled ? 'Deshabilitado' : 'Activo'}
    </span>
  )
}
