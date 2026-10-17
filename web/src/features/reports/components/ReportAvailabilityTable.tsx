import { Cell, DataTable, HeadCell, HeadRow, Row, RowNumberCell, RowNumberHeadCell, TableBody, TableHeader } from '@/components/data-table/DataTable'
import { AvailabilityBadge } from './AvailabilityBadge'
import { formatCoveragePct, formatSeconds, formatTrackedFrom } from '../utils/format'
import type { AvailabilityGroupItem, AvailabilityItem } from '../types/report.types'

interface ReportAvailabilityTableProps {
  items: AvailabilityItem[] | AvailabilityGroupItem[]
  isGroupView: boolean
}

function isAvailabilityItem(item: AvailabilityItem | AvailabilityGroupItem): item is AvailabilityItem {
  return 'hostname' in item
}

export function ReportAvailabilityTable({ items, isGroupView }: ReportAvailabilityTableProps) {
  return (
    <DataTable caption={isGroupView ? 'Disponibilidad por grupo' : 'Disponibilidad por host'} className="min-w-[70rem]">
      <TableHeader>
        <HeadRow>
          <RowNumberHeadCell />
          <HeadCell>{isGroupView ? 'Grupo' : 'Host'}</HeadCell>
          <HeadCell>Disponibilidad</HeadCell>
          <HeadCell>Tiempo caído</HeadCell>
          <HeadCell>Incidentes</HeadCell>
          <HeadCell>Cobertura</HeadCell>
          {!isGroupView && <HeadCell>Desde</HeadCell>}
        </HeadRow>
      </TableHeader>
      <TableBody>
        {items.map((item, i) => {
          const isHost = isAvailabilityItem(item)
          const name = isHost ? item.hostname : item.group_name || '(Sin grupo)'
          const key = isHost ? item.host_id : item.group_id || 'ungrouped'

          return (
            <Row key={key}>
              <RowNumberCell n={i + 1} />
              <Cell className="max-w-80 truncate font-semibold">{name}</Cell>
              <Cell>
                <AvailabilityBadge pct={item.availability_pct} />
              </Cell>
              <Cell className="tabular-nums">{formatSeconds(item.down_s)}</Cell>
              <Cell className="tabular-nums">{item.incidents}</Cell>
              <Cell className="tabular-nums">{formatCoveragePct(item.coverage_pct)}</Cell>
              {isHost && <Cell className="tabular-nums">{formatTrackedFrom(item.tracked_from)}</Cell>}
            </Row>
          )
        })}
      </TableBody>
    </DataTable>
  )
}
