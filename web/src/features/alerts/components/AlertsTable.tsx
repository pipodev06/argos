import { IconCheck, IconEye } from '@tabler/icons-react'
import { Link } from 'react-router'
import { Cell, DataTable, HeadCell, HeadRow, Row } from '@/components/data-table/DataTable'
import { SkeletonRows } from '@/components/data-table/SkeletonRows'
import { Button } from '@/components/ui/button'
import { formatLastSeen } from '@/features/hosts/utils/format'
import type { Alert } from '../types/alert.types'
import { formatAlertValue } from '../utils/format'
import type { AlertActionKind } from './AlertActionDialog'
import { AlertStateBadge, SeverityBadge } from './AlertBadges'

interface AlertsTableProps {
  /** null = cargando. */
  alerts: Alert[] | null
  now: number
  /** Solo admin ve la columna de acciones. */
  canAct: boolean
  onAction: (action: AlertActionKind, alert: Alert) => void
}

export function AlertsTable({ alerts, now, canAct, onAction }: AlertsTableProps) {
  const cols = canAct ? 7 : 6
  return (
    <DataTable caption="Alertas" className="min-w-[56rem]">
      <thead>
        <HeadRow>
          <HeadCell>Severidad</HeadCell>
          <HeadCell>Host</HeadCell>
          <HeadCell>Regla</HeadCell>
          <HeadCell>Estado</HeadCell>
          <HeadCell>Valor</HeadCell>
          <HeadCell>Abierta</HeadCell>
          {canAct && <HeadCell>Acciones</HeadCell>}
        </HeadRow>
      </thead>
      <tbody>
        {alerts === null && <SkeletonRows colSpan={cols} />}
        {alerts?.map((a) => (
          <Row key={a.id}>
            <Cell>
              <SeverityBadge severity={a.severity} />
            </Cell>
            <Cell className="max-w-56">
              <Link
                to={`/hosts/${a.host.id}`}
                className="block truncate rounded-md font-semibold outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
              >
                {a.host.hostname}
              </Link>
            </Cell>
            <Cell className="max-w-64">
              <Link
                to={`/alertas/${a.id}`}
                className="block truncate rounded-md underline outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
                title="Ver detalle"
              >
                {a.rule_name}
              </Link>
            </Cell>
            <Cell>
              <AlertStateBadge alert={a} />
            </Cell>
            <Cell className="tabular-nums">{formatAlertValue(a)}</Cell>
            <Cell className="tabular-nums">{formatLastSeen(a.opened_at, now)}</Cell>
            {canAct && (
              <Cell>
                {a.state === 'open' && (
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`Reconocer alerta ${a.rule_name} en ${a.host.hostname}`}
                    title="Reconocer"
                    onClick={() => onAction('ack', a)}
                  >
                    <IconEye stroke={1.5} aria-hidden />
                  </Button>
                )}
                {a.state !== 'resolved' && (
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`Resolver alerta ${a.rule_name} en ${a.host.hostname}`}
                    title="Resolver"
                    onClick={() => onAction('resolve', a)}
                  >
                    <IconCheck stroke={1.5} aria-hidden />
                  </Button>
                )}
              </Cell>
            )}
          </Row>
        ))}
      </tbody>
    </DataTable>
  )
}
