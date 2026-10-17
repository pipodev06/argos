import { PencilIcon, Trash2Icon } from 'lucide-react'
import { Link } from 'react-router'
import { Cell, DataTable, HeadCell, HeadRow, Row, RowHeadCell, RowNumberCell, RowNumberHeadCell } from '@/components/data-table/DataTable'
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
  return (
    <DataTable caption="Grupos de hosts" className="min-w-[48rem]">
        <thead>
          <HeadRow>
            <RowNumberHeadCell />
            <HeadCell>Nombre</HeadCell>
            <HeadCell>Hosts</HeadCell>
            <HeadCell>Comportamiento</HeadCell>
            <HeadCell>Sin conexión tras</HeadCell>
            <HeadCell>Retención de detalle</HeadCell>
            {canEdit && (
              <HeadCell className="w-32">
                <span className="sr-only">Acciones</span>
              </HeadCell>
            )}
          </HeadRow>
        </thead>
        <tbody>
          {groups.map((g, i) => (
            <Row key={g.id}>
              <RowNumberCell n={i + 1} />
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
              <Cell className="tabular-nums">
                {g.effective_detail_days} días
                {g.retention_source === 'global' && <span className="text-text-muted"> (global)</span>}
              </Cell>
              {canEdit && (
                <Cell>
                  <Button variant="ghost" size="icon-sm" aria-label={`Editar ${g.name}`} onClick={() => onEdit(g)}>
                    <PencilIcon strokeWidth={1.5} aria-hidden />
                  </Button>
                  <Button variant="ghost" size="icon-sm" aria-label={`Eliminar ${g.name}`} onClick={() => onDelete(g)}>
                    <Trash2Icon strokeWidth={1.5} aria-hidden />
                  </Button>
                </Cell>
              )}
            </Row>
          ))}
          <Row>
            <Cell />
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
            <Cell className="text-text-muted">Global</Cell>
            {canEdit && <Cell />}
          </Row>
        </tbody>
    </DataTable>
  )
}
