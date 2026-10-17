import { IconCircleCheck, IconCircleOff } from '@tabler/icons-react'
import { Badge } from '@/components/ui/badge'

export function UserStatusBadge({ disabled }: { disabled: boolean }) {
  return disabled ? (
    <Badge Icon={IconCircleOff}>Deshabilitado</Badge>
  ) : (
    <Badge Icon={IconCircleCheck} tint="ok">
      Activo
    </Badge>
  )
}
