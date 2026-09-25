import { Link } from 'react-router'
import { Cell, DataTable, HeadCell, HeadRow, Row, RowNumberCell, RowNumberHeadCell } from '@/components/data-table/DataTable'
import { SkeletonRows } from '@/components/data-table/SkeletonRows'
import { SortableHead, type SortDir } from '@/components/data-table/SortableHead'
import { formatOs } from '@/features/hosts/utils/os'
import { rowNumber } from '@/lib/pagination'
import type { FleetItem } from '../types/updates.types'
import type { SortKey } from '../utils/fleetQuery'
import { countText, lastInstalledText } from '../utils/updates'
import { DataAgeText, EolText, RebootText } from './UpdatesBadges'

interface FleetTableProps {
  items: FleetItem[] | null
  page: number
  pageSize: number
  sort: SortKey
  dir: SortDir
  onSort: (key: SortKey) => void
  now: number
}

/** Un conteo `null` es "Desconocido" en muted, nunca 0. */
function Count({ n }: { n: number | null }) {
  return n === null ? <span className="text-text-muted">{countText(n)}</span> : <span className="tabular-nums">{countText(n)}</span>
}

export function FleetTable({ items, page, pageSize, sort, dir, onSort, now }: FleetTableProps) {
  const sp = { sort, dir, onSort }
  return (
    <DataTable caption="Parches del sistema operativo">
      <thead>
        <HeadRow>
          <RowNumberHeadCell />
          <SortableHead id="name" label="Host" {...sp} />
          <HeadCell className="hidden md:table-cell">SO</HeadCell>
          <SortableHead id="pending" label="Pendientes" {...sp} />
          <SortableHead id="security" label="Seguridad" {...sp} />
          <SortableHead id="reboot" label="Reinicio pendiente" {...sp} />
          <HeadCell className="hidden lg:table-cell">Última actualización</HeadCell>
          <SortableHead id="eol" label="Fin de vida" {...sp} />
          <SortableHead id="updated" label="Dato" {...sp} />
        </HeadRow>
      </thead>
      <tbody>
        {items === null ? (
          <SkeletonRows colSpan={9} />
        ) : (
          items.map((it, i) => (
            <Row key={it.host.id} className="relative hover:bg-muted/50">
              <RowNumberCell n={rowNumber(page, pageSize, i)} />
              <Cell className="max-w-64">
                <Link
                  to={`/hosts/${it.host.id}`}
                  className="block min-w-0 truncate rounded-md font-semibold outline-none after:absolute after:inset-0 focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
                >
                  {it.host.hostname}
                </Link>
              </Cell>
              <Cell className="hidden md:table-cell">{formatOs(it.os, it.os_name, it.os_version)}</Cell>
              <Cell><Count n={it.pending_total} /></Cell>
              <Cell><Count n={it.pending_security} /></Cell>
              <Cell><RebootText required={it.reboot_required} since={it.reboot_since} now={now} /></Cell>
              <Cell className="hidden tabular-nums lg:table-cell">
                {it.last_installed ? lastInstalledText(it.last_installed) : <span className="text-text-muted">Desconocido</span>}
              </Cell>
              <Cell><EolText eol={it.eol} /></Cell>
              <Cell className="text-xs text-text-muted"><DataAgeText seconds={it.data_age_s} stale={it.stale} /></Cell>
            </Row>
          ))
        )}
      </tbody>
    </DataTable>
  )
}
