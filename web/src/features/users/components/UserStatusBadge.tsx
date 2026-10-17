import { CircleCheckIcon, CircleOffIcon } from 'lucide-react'
import { Badge } from '@/components/ui/badge'

export function UserStatusBadge({ disabled }: { disabled: boolean }) {
  return disabled ? (
    <Badge Icon={CircleOffIcon}>Deshabilitado</Badge>
  ) : (
    <Badge Icon={CircleCheckIcon} tint="ok">
      Activo
    </Badge>
  )
}
