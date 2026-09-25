import { Cell, DataTable, HeadCell, HeadRow, Row, RowNumberCell, RowNumberHeadCell } from '@/components/data-table/DataTable'
import { AvailabilityBadge } from './AvailabilityBadge'
import { formatCoveragePct, formatSeconds } from '../utils/format'
import type { ServiceCheckItem } from '../types/report.types'

interface ReportServiceChecksTableProps {
  items: ServiceCheckItem[]
}

export function ReportServiceChecksTable({ items }: ReportServiceChecksTableProps) {
  return (
    <DataTable caption="Disponibilidad de chequeos de servicio" className="min-w-[80rem]">
      <thead>
        <HeadRow>
          <RowNumberHeadCell />
          <HeadCell>Host</HeadCell>
          <HeadCell>Chequeo</HeadCell>
          <HeadCell>Disponibilidad</HeadCell>
          <HeadCell>Degradado</HeadCell>
          <HeadCell>Caído</HeadCell>
          <HeadCell>Incidentes</HeadCell>
          <HeadCell>Cobertura</HeadCell>
        </HeadRow>
      </thead>
      <tbody>
        {items.map((item, i) => {

          return (
            <Row key={`${item.host_id}-${item.check_id}`}>
              <RowNumberCell n={i + 1} />
              <Cell className="max-w-56 truncate font-semibold">{item.hostname}</Cell>
              <Cell className="max-w-56 truncate">{item.check_name}</Cell>
              <Cell>
                <AvailabilityBadge pct={item.availability_pct} />
              </Cell>
              <Cell className="tabular-nums">{formatSeconds(item.warn_s)}</Cell>
              <Cell className="tabular-nums">{formatSeconds(item.down_s)}</Cell>
              <Cell className="tabular-nums">{item.incidents}</Cell>
              <Cell className="tabular-nums">{formatCoveragePct(item.coverage_pct)}</Cell>
            </Row>
          )
        })}
      </tbody>
    </DataTable>
  )
}
