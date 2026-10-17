import { Link } from 'react-router'
import { Cell, DataTable, HeadCell, HeadRow, Row, RowHeadCell, RowNumberCell, RowNumberHeadCell, TableBody, TableHeader } from '@/components/data-table/DataTable'
import type { Group } from '@/features/groups/types/group.types'
import { sourceLabel } from '../utils/retention'

/** Grupos con retención propia. El servidor no lista hosts con retención propia: se ven en el detalle de cada host. */
export function RetentionExceptions({ groups }: { groups: Group[] }) {
  const own = groups.filter((g) => g.detail_retention_days !== null)
  return (
    <section aria-labelledby="exc-title" className="flex flex-col gap-3">
      <div className="flex flex-col gap-1">
        <h2 id="exc-title" className="text-sm font-semibold">Excepciones</h2>
        <p className="text-sm text-text-muted">
          Grupos con retención propia. La retención de un host se edita y se ve en su detalle (Editar).
        </p>
      </div>
      {own.length === 0 ? (
        <p className="text-sm text-text-muted">Ningún grupo tiene retención propia: todos usan la global.</p>
      ) : (
        <DataTable caption="Grupos con retención de detalle propia" className="min-w-[36rem]">
          <TableHeader>
            <HeadRow>
              <RowNumberHeadCell />
              <HeadCell>Nombre</HeadCell>
              <HeadCell>Ámbito</HeadCell>
              <HeadCell>Retención propia</HeadCell>
              <HeadCell>Efectiva</HeadCell>
              <HeadCell>Origen</HeadCell>
            </HeadRow>
          </TableHeader>
          <TableBody>
            {own.map((g, i) => (
              <Row key={g.id}>
                <RowNumberCell n={i + 1} />
                <RowHeadCell>
                  <Link to={`/hosts?group=${g.id}`} className="underline-offset-2 hover:underline">{g.name}</Link>
                </RowHeadCell>
                <Cell>Grupo</Cell>
                <Cell className="tabular-nums">{g.detail_retention_days} días</Cell>
                <Cell className="tabular-nums">{g.effective_detail_days} días</Cell>
                <Cell>{sourceLabel(g.retention_source)}</Cell>
              </Row>
            ))}
          </TableBody>
        </DataTable>
      )}
    </section>
  )
}
