import { Cell, DataTable, HeadCell, HeadRow, Row } from '@/components/data-table/DataTable'
import { formatAvailabilityPercent, formatSeconds } from '../utils/format'
import type { ServiceCheckItem } from '../types/report.types'

interface ReportServiceChecksTableProps {
  items: ServiceCheckItem[]
}

const BADGE_BASE = 'inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-xs font-medium text-text'

export function ReportServiceChecksTable({ items }: ReportServiceChecksTableProps) {
  return (
    <DataTable caption="Disponibilidad de chequeos de servicio" className="min-w-[80rem]">
      <thead>
        <HeadRow>
          <HeadCell>Host</HeadCell>
          <HeadCell>Chequeo</HeadCell>
          <HeadCell className="text-center">Disponibilidad</HeadCell>
          <HeadCell className="text-right">Degradado</HeadCell>
          <HeadCell className="text-right">Caído</HeadCell>
          <HeadCell className="text-right">Incidentes</HeadCell>
          <HeadCell className="text-right">Cobertura</HeadCell>
        </HeadRow>
      </thead>
      <tbody>
        {items.map((item) => {
          const badgeClass =
            item.availability_pct === null ? `${BADGE_BASE} bg-text-muted/10` : item.availability_pct >= 0.95 ? `${BADGE_BASE} bg-ok/10` : `${BADGE_BASE} bg-warn/10`

          return (
            <Row key={`${item.host_id}-${item.check_id}`}>
              <Cell className="max-w-56 truncate font-semibold">{item.hostname}</Cell>
              <Cell className="max-w-56 truncate">{item.check_name}</Cell>
              <Cell className="text-center">
                <span className={badgeClass}>{formatAvailabilityPercent(item.availability_pct)}</span>
              </Cell>
              <Cell className="tabular-nums text-right">{formatSeconds(item.warn_s)}</Cell>
              <Cell className="tabular-nums text-right">{formatSeconds(item.down_s)}</Cell>
              <Cell className="tabular-nums text-right">{item.incidents}</Cell>
              <Cell className="text-right text-xs text-text-muted">{Math.round(item.coverage_pct)}%</Cell>
            </Row>
          )
        })}
      </tbody>
    </DataTable>
  )
}
