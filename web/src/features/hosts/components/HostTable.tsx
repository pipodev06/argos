import { IconAlertTriangle, IconBattery, IconBatteryCharging } from '@tabler/icons-react'
import { cn } from 'cn'
import { Link } from 'react-router'
import { Checkbox } from '@/components/ui/checkbox'
import { Cell, DataTable, HeadCell, HeadRow, Row } from '@/components/data-table/DataTable'
import { SkeletonRows } from '@/components/data-table/SkeletonRows'
import { SortableHead, type SortDir } from '@/components/data-table/SortableHead'
import { usageLevel } from '@/lib/usage'
import type { Host } from '../types/host.types'
import type { OptionalColumn } from '../utils/columns'
import { formatElapsed, formatLastSeen, formatNumber } from '../utils/format'
import type { SortKey } from '../utils/hostQuery'
import { formatOs } from '../utils/os'
import { HostStatusBadge } from './HostStatusBadge'

interface HostTableProps {
  hosts: Host[] | null
  visible: readonly OptionalColumn[]
  sort: SortKey
  dir: SortDir
  onSort: (key: SortKey) => void
  /** null = sin seleccion (viewer o movil). */
  selected: ReadonlySet<string> | null
  onToggle: (id: string, on: boolean) => void
  onToggleAll: (on: boolean) => void
}

function Pct({ value }: { value: number | null }) {
  if (value === null) return <span className="text-text-muted">—</span>
  const high = usageLevel(value) !== 'ok'
  return (
    <span className="inline-flex items-center justify-center gap-1 tabular-nums">
      {high && <IconAlertTriangle className="size-3.5 shrink-0 text-warn" stroke={1.5} aria-label="Uso alto" />}
      {formatNumber(value)} %
    </span>
  )
}

export function HostTable({ hosts, visible, sort, dir, onSort, selected, onToggle, onToggleAll }: HostTableProps) {
  const show = (c: OptionalColumn) => visible.includes(c)
  const sortProps = { sort, dir, onSort }
  const allSelected = hosts !== null && hosts.length > 0 && selected !== null && hosts.every((h) => selected.has(h.id))
  const someSelected = hosts !== null && selected !== null && hosts.some((h) => selected.has(h.id))
  const colSpan = 8 + visible.length + 1

  return (
    <DataTable caption="Hosts">
        <thead>
          <HeadRow>
            {selected && (
              <th scope="col" className="hidden w-10 px-3 md:table-cell">
                <Checkbox
                  aria-label="Seleccionar todos los de esta página"
                  checked={allSelected ? true : someSelected ? 'indeterminate' : false}
                  onCheckedChange={(v) => onToggleAll(v === true)}
                />
              </th>
            )}
            <SortableHead id="name" label="Host" {...sortProps} />
            <SortableHead id="status" label="Estado" {...sortProps} />
            <SortableHead id="group" label="Grupo" {...sortProps} className="hidden md:table-cell" />
            <HeadCell className="hidden md:table-cell">SO</HeadCell>
            <SortableHead id="cpu" label="CPU" {...sortProps} className="hidden md:table-cell" />
            <SortableHead id="mem" label="Mem" {...sortProps} className="hidden md:table-cell" />
            <SortableHead id="disk" label="Disco (uso)" {...sortProps} className="hidden md:table-cell" />
            {show('ip') && <HeadCell className="hidden md:table-cell">IP</HeadCell>}
            {show('uptime') && <HeadCell className="hidden md:table-cell">Uptime</HeadCell>}
            {show('battery') && <HeadCell className="hidden md:table-cell">Batería</HeadCell>}
            <SortableHead id="last_seen" label="Visto" {...sortProps} />
          </HeadRow>
        </thead>
        <tbody>
          {hosts === null
            ? <SkeletonRows colSpan={colSpan} />
            : hosts.map((h) => {
                const isSelected = selected?.has(h.id) ?? false
                return (
                  <Row key={h.id} className={cn('relative hover:bg-muted/50', isSelected && 'bg-muted')}>
                    {selected && (
                      <td className="relative z-10 hidden w-10 px-3 md:table-cell">
                        <Checkbox
                          aria-label={`Seleccionar ${h.hostname}`}
                          checked={isSelected}
                          onCheckedChange={(v) => onToggle(h.id, v === true)}
                        />
                      </td>
                    )}
                    <Cell className="relative max-w-64">
                      {h.status === 'down' && (
                        <span aria-hidden className="absolute inset-y-0 left-0 w-0.5 bg-danger" />
                      )}
                      <Link
                        to={`/hosts/${h.id}`}
                        className="block truncate rounded-md font-semibold outline-none after:absolute after:inset-0 focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
                      >
                        {h.hostname}
                      </Link>
                      <span className="block truncate text-xs text-text-muted md:hidden">
                        {[h.group?.name ?? 'Sin grupo', formatOs(h.os, h.os_name, h.os_version)].filter(Boolean).join(' · ')}
                      </span>
                    </Cell>
                    <Cell>
                      <HostStatusBadge status={h.status} />
                    </Cell>
                    <Cell className="hidden max-w-40 truncate md:table-cell">
                      {h.group?.name ?? <span className="text-text-muted">Sin grupo</span>}
                    </Cell>
                    <Cell className="hidden md:table-cell">{formatOs(h.os, h.os_name, h.os_version)}</Cell>
                    <Cell className="hidden text-center md:table-cell"><Pct value={h.cpu_pct} /></Cell>
                    <Cell className="hidden text-center md:table-cell"><Pct value={h.mem_pct} /></Cell>
                    <Cell className="hidden text-center md:table-cell">
                      <Pct value={h.disk?.pct ?? null} />
                      {h.disk && <span className="ml-1 text-xs text-text-muted">{h.disk.mount}</span>}
                    </Cell>
                    {show('ip') && (
                      <Cell className="hidden md:table-cell">{h.ip ?? '—'}</Cell>
                    )}
                    {show('uptime') && (
                      <Cell className="hidden text-center tabular-nums md:table-cell">
                        {h.boot_time && h.status === 'up' ? formatElapsed(h.boot_time) : '—'}
                      </Cell>
                    )}
                    {show('battery') && (
                      <Cell className="hidden text-center tabular-nums md:table-cell">
                        {h.battery ? (
                          <span className="inline-flex items-center gap-1">
                            {h.battery.charging ? (
                              <IconBatteryCharging className="size-4" stroke={1.5} aria-label="Cargando" />
                            ) : (
                              <IconBattery className="size-4" stroke={1.5} aria-hidden />
                            )}
                            {h.battery.pct} %
                          </span>
                        ) : (
                          '—'
                        )}
                      </Cell>
                    )}
                    <Cell className="text-center text-xs text-text-muted tabular-nums">
                      {formatLastSeen(h.last_seen_at)}
                    </Cell>
                  </Row>
                )
              })}
        </tbody>
    </DataTable>
  )
}
