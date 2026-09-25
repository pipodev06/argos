import { IconCalendarEvent, IconCircleCheck, IconTool, IconPencil, IconTrash } from '@tabler/icons-react'
import type { ComponentType } from 'react'
import { Cell, DataTable, HeadCell, HeadRow, Row, RowNumberCell, RowNumberHeadCell } from '@/components/data-table/DataTable'
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

type BadgeIcon = ComponentType<{ className?: string; stroke?: number; 'aria-hidden'?: boolean }>
const statusStyles: Record<MaintenanceStatus, { Icon: BadgeIcon; badge: string; icon: string }> = {
  active: { Icon: IconTool, badge: 'bg-info/10', icon: 'text-info' },
  scheduled: { Icon: IconCalendarEvent, badge: 'bg-text-muted/10', icon: 'text-text-muted' },
  ended: { Icon: IconCircleCheck, badge: 'bg-text-muted/10', icon: 'text-text-muted' },
}

function MaintenanceStatusBadge({ status }: { status: MaintenanceStatus }) {
  const { Icon, badge, icon } = statusStyles[status]
  return (
    <span className={`inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-xs font-medium text-text ${badge}`}>
      <Icon className={`size-3.5 shrink-0 ${icon}`} stroke={1.5} aria-hidden />
      {MAINTENANCE_STATUS_LABELS[status]}
    </span>
  )
}

export function MaintenanceTable({ windows, canAct, onAction }: MaintenanceTableProps) {
  return (
    <DataTable caption="Ventanas de mantenimiento" className="min-w-[52rem]">
      <thead>
        <HeadRow>
          <RowNumberHeadCell />
          <HeadCell>Nombre</HeadCell>
          <HeadCell>Ámbito</HeadCell>
          <HeadCell>Inicio</HeadCell>
          <HeadCell>Fin</HeadCell>
          <HeadCell>Estado</HeadCell>
          {canAct && <HeadCell>Acciones</HeadCell>}
        </HeadRow>
      </thead>
      <tbody>
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
                  <IconPencil stroke={1.5} aria-hidden />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`Eliminar ventana ${w.name}`}
                  title="Eliminar"
                  onClick={() => onAction('delete', w)}
                >
                  <IconTrash stroke={1.5} aria-hidden />
                </Button>
              </Cell>
            )}
          </Row>
        ))}
      </tbody>
    </DataTable>
  )
}
