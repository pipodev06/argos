import { CalendarClockIcon, CircleCheckIcon, WrenchIcon, PencilIcon, Trash2Icon, type LucideIcon as Icon } from 'lucide-react'
import { Cell, DataTable, HeadCell, HeadRow, Row, RowNumberCell, RowNumberHeadCell, TableBody, TableHeader } from '@/components/data-table/DataTable'
import { Badge, type BadgeTint } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { formatFullDate } from '@/features/hosts/utils/format'
import type { MaintenanceStatus, MaintenanceWindow } from '../types/alert.types'
import { MAINTENANCE_STATUS_LABELS, scopeLabel } from '../utils/format'

export type MaintenanceAction = 'edit' | 'delete'

interface MaintenanceTableProps {
  windows: MaintenanceWindow[]
  /** Solo admin ve la columna de acciones. */
  canAct: boolean
  onAction: (action: MaintenanceAction, window: MaintenanceWindow) => void
}

const statusStyles: Record<MaintenanceStatus, { Icon: Icon; tint: BadgeTint }> = {
  active: { Icon: WrenchIcon, tint: 'info' },
  scheduled: { Icon: CalendarClockIcon, tint: 'muted' },
  ended: { Icon: CircleCheckIcon, tint: 'muted' },
}

function MaintenanceStatusBadge({ status }: { status: MaintenanceStatus }) {
  const { Icon, tint } = statusStyles[status]
  return (
    <Badge Icon={Icon} tint={tint}>
      {MAINTENANCE_STATUS_LABELS[status]}
    </Badge>
  )
}

export function MaintenanceTable({ windows, canAct, onAction }: MaintenanceTableProps) {
  return (
    <DataTable caption="Ventanas de mantenimiento" className="min-w-[52rem]">
      <TableHeader>
        <HeadRow>
          <RowNumberHeadCell />
          <HeadCell>Nombre</HeadCell>
          <HeadCell>Ámbito</HeadCell>
          <HeadCell>Inicio</HeadCell>
          <HeadCell>Fin</HeadCell>
          <HeadCell>Estado</HeadCell>
          {canAct && <HeadCell>Acciones</HeadCell>}
        </HeadRow>
      </TableHeader>
      <TableBody>
        {windows.map((w, i) => (
          <Row key={w.id}>
            <RowNumberCell n={i + 1} />
            <Cell className="max-w-56 truncate font-semibold">{w.name}</Cell>
            <Cell className="max-w-48 truncate">{scopeLabel(w.scope)}</Cell>
            <Cell className="tabular-nums">{formatFullDate(w.starts_at)}</Cell>
            <Cell className="tabular-nums">{formatFullDate(w.ends_at)}</Cell>
            <Cell>
              <MaintenanceStatusBadge status={w.status} />
            </Cell>
            {canAct && (
              <Cell>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`Editar ventana ${w.name}`}
                  title="Editar"
                  onClick={() => onAction('edit', w)}
                >
                  <PencilIcon strokeWidth={1.5} aria-hidden />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`Eliminar ventana ${w.name}`}
                  title="Eliminar"
                  onClick={() => onAction('delete', w)}
                >
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
