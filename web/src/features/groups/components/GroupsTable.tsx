import { IconPencil, IconTrash } from '@tabler/icons-react'
import { Link } from 'react-router'
import { Cell, DataTable, HeadCell, HeadRow, Row, RowHeadCell } from '@/components/data-table/DataTable'
import { Button } from '@/components/ui/button'
import type { Group } from '../types/group.types'
import { formatThreshold } from '../utils/format'

interface GroupsTableProps {
  groups: Group[]
  ungroupedCount: number
  defaultDownAfter: number
  canEdit: boolean
  onEdit: (group: Group) => void
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
  return (
    <DataTable caption="Grupos de hosts" className="min-w-[40rem]">
        <thead>
          <HeadRow>
            <HeadCell>Nombre</HeadCell>
            <HeadCell>Hosts</HeadCell>
            <HeadCell>Comportamiento</HeadCell>
            <HeadCell>Sin conexión tras</HeadCell>
            {canEdit && (
              <HeadCell className="w-24">
                <span className="sr-only">Acciones</span>
              </HeadCell>
            )}
          </HeadRow>
        </thead>
        <tbody>
          {groups.map((g) => (
            <Row key={g.id}>
              <RowHeadCell>{g.name}</RowHeadCell>
              <Cell className="tabular-nums">
                <Link to={`/hosts?group=${g.id}`} className="underline-offset-2 hover:underline">
                  {g.host_count.toLocaleString('es-PE')}
                </Link>
              </Cell>
              <Cell>{g.always_on ? 'Caído (siempre encendido)' : 'Sin conexión (normal)'}</Cell>
              <Cell className="tabular-nums">
                {formatThreshold(g.down_after_seconds ?? defaultDownAfter)}
                {g.down_after_seconds === null && <span className="text-text-muted"> (global)</span>}
              </Cell>
              {canEdit && (
                <Cell>
                  <Button variant="ghost" size="icon-sm" aria-label={`Editar ${g.name}`} onClick={() => onEdit(g)}>
                    <IconPencil stroke={1.5} aria-hidden />
                  </Button>
                  <Button variant="ghost" size="icon-sm" aria-label={`Eliminar ${g.name}`} onClick={() => onDelete(g)}>
                    <IconTrash stroke={1.5} aria-hidden />
                  </Button>
                </Cell>
              )}
            </Row>
          ))}
          <Row>
            <RowHeadCell className="text-text-muted">Sin grupo</RowHeadCell>
            <Cell className="tabular-nums">
              <Link to="/hosts?group=none" className="underline-offset-2 hover:underline">
                {ungroupedCount.toLocaleString('es-PE')}
              </Link>
            </Cell>
            <Cell className="text-text-muted">Sin conexión (normal)</Cell>
            <Cell className="tabular-nums">
              {formatThreshold(defaultDownAfter)}
              <span className="text-text-muted"> (global)</span>
            </Cell>
            {canEdit && <Cell />}
          </Row>
        </tbody>
    </DataTable>
  )
}
