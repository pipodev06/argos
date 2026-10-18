import { PencilIcon, Trash2Icon } from 'lucide-react'
import { Cell, DataTable, HeadCell, HeadRow, Row, RowHeadCell, RowNumberCell, RowNumberHeadCell, TableBody, TableHeader } from '@/components/data-table/DataTable'
import { ActionsCell, ActionsHeadCell, ViewAction } from '@/components/data-table/RowActions'
import { Button } from '@/components/ui/button'
import type { Group } from '../types/group.types'
import { formatThreshold } from '../utils/format'

interface GroupsTableProps {
  groups: Group[]
  ungroupedCount: number
  defaultDownAfter: number
  canEdit: boolean
  onEdit: (group: Group) => void
  /** Vacío: borra el grupo. Con hosts: abre la baja (purga) de todo el grupo. */
  onDelete: (group: Group) => void
}

export function GroupsTable({
  groups,
  ungroupedCount,
  defaultDownAfter,
  canEdit,
  onEdit,
  onDelete,
}: GroupsTableProps) {
  // Los hosts sin grupo usan la retención global: la misma que muestra cualquier grupo que la hereda.
  const globalDays = groups.find((g) => g.retention_source === 'global')?.effective_detail_days ?? null
  return (
    <DataTable caption="Grupos de hosts" className="min-w-[48rem]">
        <TableHeader>
          <HeadRow>
            <RowNumberHeadCell />
            <HeadCell>Nombre</HeadCell>
            <HeadCell>Hosts</HeadCell>
            <HeadCell>Comportamiento</HeadCell>
            <HeadCell>Sin conexión tras</HeadCell>
            <HeadCell>Retención de detalle</HeadCell>
            <ActionsHeadCell />
          </HeadRow>
        </TableHeader>
        <TableBody>
          {groups.map((g, i) => (
            <Row key={g.id}>
              <RowNumberCell n={i + 1} />
              <RowHeadCell>{g.name}</RowHeadCell>
              <Cell className="tabular-nums">{g.host_count.toLocaleString('es-PE')}</Cell>
              <Cell>{g.always_on ? 'Caído' : 'Sin conexión'}</Cell>
              <Cell className="tabular-nums">
                {formatThreshold(g.down_after_seconds ?? defaultDownAfter)}
              </Cell>
              <Cell className="tabular-nums">{g.effective_detail_days} días</Cell>
              <ActionsCell>
                <ViewAction to={`/hosts?group=${g.id}`} label={`hosts de ${g.name}`} />
                {canEdit && (
                <>
                  <Button variant="ghost" size="icon-sm" aria-label={`Editar ${g.name}`} onClick={() => onEdit(g)}>
                    <PencilIcon strokeWidth={1.5} aria-hidden />
                  </Button>
                  <Button variant="ghost" size="icon-sm" aria-label={`Eliminar ${g.name}`} onClick={() => onDelete(g)}>
                    <Trash2Icon strokeWidth={1.5} aria-hidden />
                  </Button>
                </>
                )}
              </ActionsCell>
            </Row>
          ))}
          <Row>
            <Cell />
            <RowHeadCell className="text-text-muted">Sin grupo</RowHeadCell>
            <Cell className="tabular-nums">{ungroupedCount.toLocaleString('es-PE')}</Cell>
            <Cell>Sin conexión</Cell>
            <Cell className="tabular-nums">{formatThreshold(defaultDownAfter)}</Cell>
            <Cell className="tabular-nums">{globalDays === null ? '—' : `${globalDays} días`}</Cell>
            <ActionsCell>
              <ViewAction to="/hosts?group=none" label="hosts sin grupo" />
            </ActionsCell>
          </Row>
        </TableBody>
    </DataTable>
  )
}
