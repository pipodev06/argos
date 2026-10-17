import { PencilIcon, PauseIcon, PlayIcon, Trash2Icon } from 'lucide-react'
import { Link } from 'react-router'
import { Cell, DataTable, HeadCell, HeadRow, Row, RowNumberCell, RowNumberHeadCell, TableBody, TableHeader } from '@/components/data-table/DataTable'
import { SkeletonRows } from '@/components/data-table/SkeletonRows'
import { Button } from '@/components/ui/button'
import { formatLastSeen } from '@/features/hosts/utils/format'
import { rowNumber } from '@/lib/pagination'
import type { Device } from '../types/device.types'
import { CollectorMismatchNotice, DeviceStatusBadge } from './DeviceBadges'

export type DeviceAction = 'edit' | 'toggle' | 'delete'

interface DeviceTableProps {
  devices: Device[] | null
  page: number
  pageSize: number
  /** Solo un admin recibe direccion y colector, y puede editar, deshabilitar/habilitar y eliminar. */
  isAdmin: boolean
  onAction: (action: DeviceAction, device: Device) => void
}

export function DeviceTable({ devices, page, pageSize, isAdmin, onAction }: DeviceTableProps) {
  const colSpan = isAdmin ? 10 : 6
  return (
    <DataTable caption="Dispositivos" className="min-w-[48rem]">
      <TableHeader>
        <HeadRow>
          <RowNumberHeadCell />
          <HeadCell>Dispositivo</HeadCell>
          {isAdmin && <HeadCell>Dirección</HeadCell>}
          {isAdmin && <HeadCell>Colector</HeadCell>}
          {isAdmin && <HeadCell>SNMP</HeadCell>}
          <HeadCell>Grupo</HeadCell>
          <HeadCell>Estado</HeadCell>
          <HeadCell>Visto</HeadCell>
          {isAdmin && <HeadCell>Acciones</HeadCell>}
        </HeadRow>
      </TableHeader>
      <TableBody>
        {devices === null ? (
          <SkeletonRows colSpan={colSpan} />
        ) : (
          devices.map((d, i) => (
            <Row key={d.id} className="relative hover:bg-muted/50">
              <RowNumberCell n={rowNumber(page, pageSize, i)} />
              <Cell className="relative max-w-64">
                {d.status === 'down' && <span aria-hidden className="absolute inset-y-0 left-0 w-0.5 bg-danger" />}
                <Link
                  to={`/hosts/${d.id}`}
                  className="block truncate rounded-md font-semibold outline-none after:absolute after:inset-0 focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
                >
                  {d.name}
                </Link>
              </Cell>
              {isAdmin && <Cell className="tabular-nums">{d.address ?? '—'}</Cell>}
              {isAdmin && <Cell className="max-w-40 truncate">{d.collector?.name ?? '—'}</Cell>}
              {isAdmin && <Cell className="max-w-40 truncate">{d.snmp_profile?.name ?? <span className="text-text-muted">—</span>}</Cell>}
              <Cell className="max-w-40 truncate">{d.group?.name ?? <span className="text-text-muted">Sin grupo</span>}</Cell>
              <Cell>
                <span className="inline-flex flex-wrap items-center justify-center gap-x-2">
                  <DeviceStatusBadge device={d} />
                  {isAdmin && d.collector_group_mismatch && <CollectorMismatchNotice compact />}
                </span>
              </Cell>
              <Cell className="text-sm text-text-muted tabular-nums">{formatLastSeen(d.last_seen_at)}</Cell>
              {isAdmin && (
                <Cell className="relative z-10">
                  <Button variant="ghost" size="icon-sm" aria-label={`Editar dispositivo ${d.name}`} title="Editar" onClick={() => onAction('edit', d)}>
                    <PencilIcon strokeWidth={1.5} aria-hidden />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`${d.status === 'disabled' ? 'Habilitar' : 'Deshabilitar'} dispositivo ${d.name}`}
                    title={d.status === 'disabled' ? 'Habilitar' : 'Deshabilitar'}
                    onClick={() => onAction('toggle', d)}
                  >
                    {d.status === 'disabled' ? <PlayIcon strokeWidth={1.5} aria-hidden /> : <PauseIcon strokeWidth={1.5} aria-hidden />}
                  </Button>
                  <Button variant="ghost" size="icon-sm" aria-label={`Eliminar dispositivo ${d.name}`} title="Eliminar" onClick={() => onAction('delete', d)}>
                    <Trash2Icon strokeWidth={1.5} aria-hidden />
                  </Button>
                </Cell>
              )}
            </Row>
          ))
        )}
      </TableBody>
    </DataTable>
  )
}
