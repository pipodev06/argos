import { PencilIcon, PauseIcon, PlayIcon, Trash2Icon } from 'lucide-react'
import { Cell, DataTable, HeadCell, HeadRow, Row, RowNumberCell, RowNumberHeadCell, TableBody, TableHeader } from '@/components/data-table/DataTable'
import { ActionsHeadCell } from '@/components/data-table/RowActions'
import { Button } from '@/components/ui/button'
import { RuleEnabledBadge } from '@/features/alerts/components/RulesTable'
import { formatSeconds } from '@/features/alerts/utils/format'
import type { Device } from '@/features/devices/types/device.types'
import type { ServiceCheck } from '../types/check.types'
import { checkScopeLabel, checkTarget, checkTypeLabel } from '../utils/format'
import { checkOrigin } from '../utils/origin'

export type CheckAction = 'edit' | 'toggle' | 'delete'

interface ChecksTableProps {
  checks: ServiceCheck[]
  /** Solo admin ve la columna de acciones. */
  canAct: boolean
  busy: boolean
  /** Dispositivos (para saber el colector de los ping/snmp); undefined mientras cargan. */
  devices?: Device[]
  onAction: (action: CheckAction, check: ServiceCheck) => void
}

export function ChecksTable({ checks, canAct, busy, devices, onAction }: ChecksTableProps) {
  return (
    <DataTable caption="Chequeos de servicios" className="min-w-[68rem]">
      <TableHeader>
        <HeadRow>
          <RowNumberHeadCell />
          <HeadCell>Nombre</HeadCell>
          <HeadCell>Tipo</HeadCell>
          <HeadCell>Destino</HeadCell>
          <HeadCell>Intervalo</HeadCell>
          <HeadCell>Alcance</HeadCell>
          <HeadCell>Origen</HeadCell>
          <HeadCell>Activo</HeadCell>
          {canAct && <ActionsHeadCell />}
        </HeadRow>
      </TableHeader>
      <TableBody>
        {checks.map((c, i) => (
          <Row key={c.id}>
            <RowNumberCell n={i + 1} />
            <Cell className="max-w-56 truncate font-semibold">{c.name}</Cell>
            <Cell className="max-w-56 truncate">{checkTypeLabel(c)}</Cell>
            <Cell className="max-w-64 truncate tabular-nums" title={checkTarget(c)}>
              {checkTarget(c)}
            </Cell>
            <Cell className="tabular-nums">{formatSeconds(c.interval_seconds)}</Cell>
            <Cell className="max-w-48 truncate">{checkScopeLabel(c)}</Cell>
            <Cell className="max-w-48 truncate" title={checkOrigin(c, devices)}>
              {checkOrigin(c, devices)}
            </Cell>
            <Cell>
              <RuleEnabledBadge enabled={c.enabled} />
            </Cell>
            {canAct && (
              <Cell>
                <Button variant="ghost" size="icon-sm" aria-label={`Editar chequeo ${c.name}`} title="Editar" onClick={() => onAction('edit', c)}>
                  <PencilIcon strokeWidth={1.5} aria-hidden />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`${c.enabled ? 'Desactivar' : 'Activar'} chequeo ${c.name}`}
                  title={c.enabled ? 'Desactivar' : 'Activar'}
                  disabled={busy}
                  onClick={() => onAction('toggle', c)}
                >
                  {c.enabled ? <PauseIcon strokeWidth={1.5} aria-hidden /> : <PlayIcon strokeWidth={1.5} aria-hidden />}
                </Button>
                <Button variant="ghost" size="icon-sm" aria-label={`Eliminar chequeo ${c.name}`} title="Eliminar" onClick={() => onAction('delete', c)}>
                  <Trash2Icon strokeWidth={1.5} aria-hidden />
                </Button>
              </Cell>
            )}
          </Row>
        ))}
      </TableBody>
    </DataTable>
  )
}
