import { Columns3Icon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { OPTIONAL_COLUMNS, type OptionalColumn } from '../utils/columns'

interface ColumnsMenuProps {
  visible: readonly OptionalColumn[]
  onToggle: (id: OptionalColumn, on: boolean) => void
}

export function ColumnsMenu({ visible, onToggle }: ColumnsMenuProps) {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="outline" size="sm" className="hidden md:inline-flex">
          <Columns3Icon strokeWidth={1.5} aria-hidden />
          Columnas
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" aria-label="Columnas visibles">
        {OPTIONAL_COLUMNS.map((c) => (
          <label key={c.id} className="flex min-h-8 cursor-default items-center gap-2 rounded-md px-2 hover:bg-muted">
            <Checkbox checked={visible.includes(c.id)} onCheckedChange={(v) => onToggle(c.id, v === true)} />
            {c.label}
          </label>
        ))}
      </PopoverContent>
    </Popover>
  )
}
