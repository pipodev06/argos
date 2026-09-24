import { IconArrowDown, IconArrowUp, IconArrowsSort } from '@tabler/icons-react'
import { cn } from 'cn'
import { HeadCell } from './DataTable'

export type SortDir = 'asc' | 'desc'

interface SortableHeadProps<K extends string> {
  id: K
  label: string
  sort: K
  dir: SortDir
  onSort: (key: K) => void
  className?: string
}

/** Encabezado ordenable (`aria-sort`, centrado). `sort`/`dir` = orden actual de la tabla. */
export function SortableHead<K extends string>({ id, label, sort, dir, onSort, className }: SortableHeadProps<K>) {
  const active = sort === id
  const Icon = !active ? IconArrowsSort : dir === 'asc' ? IconArrowUp : IconArrowDown
  return (
    <HeadCell aria-sort={active ? (dir === 'asc' ? 'ascending' : 'descending') : undefined} className={className}>
      <button
        type="button"
        onClick={() => onSort(id)}
        className={cn(
          'inline-flex min-h-8 items-center gap-1 rounded-md outline-none hover:text-text focus-visible:ring-2 focus-visible:ring-ring',
          active && 'text-text',
        )}
      >
        {label}
        <Icon className="size-3.5" stroke={1.5} aria-hidden />
      </button>
    </HeadCell>
  )
}
