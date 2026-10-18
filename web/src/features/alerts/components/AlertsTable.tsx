import { CheckIcon, HandIcon } from 'lucide-react'
import { Cell, DataTable, HeadCell, HeadRow, Row, RowNumberCell, RowNumberHeadCell, TableBody, TableHeader } from '@/components/data-table/DataTable'
import { ActionsCell, ActionsHeadCell, ViewAction } from '@/components/data-table/RowActions'
import { SkeletonRows } from '@/components/data-table/SkeletonRows'
import { Button } from '@/components/ui/button'
import { rowNumber } from '@/lib/pagination'
import { formatLastSeen } from '@/features/hosts/utils/format'
import type { Alert } from '../types/alert.types'
import { exposedPortsText, formatAlertValue } from '../utils/format'
import type { AlertActionKind } from './AlertActionDialog'
import { AlertStateBadge, EscalatedBadge, FlappingBadge, SeverityBadge } from './AlertBadges'

interface AlertsTableProps {
  /** null = cargando. */
  alerts: Alert[] | null
  page: number
  pageSize: number
  now: number
  /** Reconocer: admin o viewer con can_ack. La columna de acciones existe si hay alguna acción. */
  canAck: boolean
  /** Resolver: solo admin. */
  canResolve: boolean
  onAction: (action: AlertActionKind, alert: Alert) => void
}

export function AlertsTable({ alerts, page, pageSize, now, canAck, canResolve, onAction }: AlertsTableProps) {
  const cols = 8
  return (
    <DataTable caption="Alertas" className="min-w-[60rem]">
      <TableHeader>
        <HeadRow>
          <RowNumberHeadCell />
          <HeadCell>Severidad</HeadCell>
          <HeadCell>Host</HeadCell>
          <HeadCell>Regla</HeadCell>
          <HeadCell>Estado</HeadCell>
          <HeadCell>Valor</HeadCell>
          <HeadCell>Abierta</HeadCell>
          <ActionsHeadCell />
        </HeadRow>
      </TableHeader>
      <TableBody>
        {alerts === null && <SkeletonRows colSpan={cols} />}
        {alerts?.map((a, i) => (
          <Row key={a.id}>
            <RowNumberCell n={rowNumber(page, pageSize, i)} />
            <Cell>
              <SeverityBadge severity={a.severity} />
            </Cell>
            <Cell className="max-w-56">
              <span className="block truncate font-semibold">{a.host.hostname}</span>
            </Cell>
            <Cell className="max-w-64">
              <span className="block truncate">{a.rule_name}</span>
            </Cell>
            <Cell>
              <span className="inline-flex flex-wrap items-center justify-center gap-1">
                <AlertStateBadge alert={a} />
                <FlappingBadge flapping={a.flapping} />
                <EscalatedBadge escalatedAt={a.escalated_at} />
              </span>
            </Cell>
            <Cell className="tabular-nums">{a.kind === 'exposed_port' ? exposedPortsText(a) : formatAlertValue(a)}</Cell>
            <Cell className="tabular-nums">{formatLastSeen(a.opened_at, now)}</Cell>
            <ActionsCell>
                <ViewAction to={`/alertas/${a.id}`} label={`alerta ${a.rule_name} en ${a.host.hostname}`} />
                {canAck && a.state === 'open' && (
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`Reconocer alerta ${a.rule_name} en ${a.host.hostname}`}
                    title="Reconocer"
                    onClick={() => onAction('ack', a)}
                  >
                    <HandIcon strokeWidth={1.5} aria-hidden />
                  </Button>
                )}
                {canResolve && a.state !== 'resolved' && (
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`Resolver alerta ${a.rule_name} en ${a.host.hostname}`}
                    title="Resolver"
                    onClick={() => onAction('resolve', a)}
                  >
                    <CheckIcon strokeWidth={1.5} aria-hidden />
                  </Button>
                )}
            </ActionsCell>
          </Row>
        ))}
      </TableBody>
    </DataTable>
  )
}
