import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import type { Group } from '../types/group.types'

const NONE = 'none'

interface GroupSelectProps {
  id?: string
  /** Id del grupo; null = "Sin grupo". */
  value: string | null
  onChange: (groupId: string | null) => void
  groups: readonly Group[]
  'aria-label'?: string
  className?: string
}

/** Selector unico de grupo (con "Sin grupo"): lo usan el alta de host, la edicion y las acciones de lote. */
export function GroupSelect({ id, value, onChange, groups, className, ...rest }: GroupSelectProps) {
  return (
    <Select value={value ?? NONE} onValueChange={(v) => onChange(v === NONE ? null : v)}>
      <SelectTrigger id={id} className={className ?? 'w-full'} aria-label={rest['aria-label']}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={NONE}>Sin grupo</SelectItem>
        {groups.map((g) => (
          <SelectItem key={g.id} value={g.id}>
            {g.name}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
