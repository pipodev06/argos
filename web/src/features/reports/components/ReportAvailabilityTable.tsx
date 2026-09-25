import { Cell, DataTable, HeadCell, HeadRow, Row } from '@/components/data-table/DataTable'
import { formatAvailabilityPercent, formatSeconds } from '../utils/format'
import type { AvailabilityGroupItem, AvailabilityItem } from '../types/report.types'

interface ReportAvailabilityTableProps {
  items: AvailabilityItem[] | AvailabilityGroupItem[]
  isGroupView: boolean
}

function isAvailabilityItem(item: AvailabilityItem | AvailabilityGroupItem): item is AvailabilityItem {
  return 'hostname' in item
}

const BADGE_BASE = 'inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-xs font-medium text-text'

export function ReportAvailabilityTable({ items, isGroupView }: ReportAvailabilityTableProps) {
  return (
    <DataTable caption={isGroupView ? 'Disponibilidad por grupo' : 'Disponibilidad por host'} className="min-w-[70rem]">
      <thead>
        <HeadRow>
          <HeadCell>{isGroupView ? 'Grupo' : 'Host'}</HeadCell>
          <HeadCell className="text-center">Disponibilidad</HeadCell>
          <HeadCell className="text-right">Tiempo caído</HeadCell>
          <HeadCell className="text-right">Incidentes</HeadCell>
          <HeadCell className="text-right">Cobertura</HeadCell>
          {!isGroupView && <HeadCell className="text-xs">Desde</HeadCell>}
        </HeadRow>
      </thead>
      <tbody>
        {items.map((item) => {
          const isHost = isAvailabilityItem(item)
          const name = isHost ? item.hostname : item.group_name || '(Sin grupo)'
          const key = isHost ? item.host_id : item.group_id || 'ungrouped'
          const badgeClass =
            item.availability_pct === null ? `${BADGE_BASE} bg-text-muted/10` : item.availability_pct >= 0.95 ? `${BADGE_BASE} bg-ok/10` : `${BADGE_BASE} bg-warn/10`

          return (
            <Row key={key}>
              <Cell className="max-w-80 truncate font-semibold">{name}</Cell>
              <Cell className="text-center">
                <span className={badgeClass}>{formatAvailabilityPercent(item.availability_pct)}</span>
              </Cell>
              <Cell className="tabular-nums text-right">{formatSeconds(item.down_s)}</Cell>
              <Cell className="tabular-nums text-right">{item.incidents}</Cell>
              <Cell className="text-right text-xs text-text-muted">{Math.round(item.coverage_pct)}%</Cell>
              {isHost && <Cell className="text-xs text-text-muted">{new Date(item.tracked_from).toLocaleDateString()}</Cell>}
            </Row>
          )
        })}
      </tbody>
    </DataTable>
  )
}
