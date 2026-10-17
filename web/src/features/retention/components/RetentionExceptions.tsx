import { Link } from 'react-router'
import { Cell, DataTable, HeadCell, HeadRow, Row, RowHeadCell, RowNumberCell, RowNumberHeadCell, TableBody, TableHeader } from '@/components/data-table/DataTable'
import type { Group } from '@/features/groups/types/group.types'

/** Grupos con retención propia. El servidor no lista hosts con retención propia: se ven en el detalle de cada host. */
export function RetentionExceptions({ groups }: { groups: Group[] }) {
  const own = groups.filter((g) => g.detail_retention_days !== null)
  if (own.length === 0) return null
  return (
    <section aria-labelledby="exc-title" className="flex flex-col gap-3">
      <h2 id="exc-title" className="text-sm font-semibold">Grupos con retención propia</h2>
      <DataTable caption="Grupos con retención de detalle propia" className="min-w-[20rem]">
          <TableHeader>
            <HeadRow>
              <RowNumberHeadCell />
              <HeadCell>Nombre</HeadCell>
              <HeadCell>Retención</HeadCell>
            </HeadRow>
          </TableHeader>
          <TableBody>
            {own.map((g, i) => (
              <Row key={g.id}>
                <RowNumberCell n={i + 1} />
                <RowHeadCell>
                  <Link to={`/hosts?group=${g.id}`} className="underline-offset-2 hover:underline">{g.name}</Link>
                </RowHeadCell>
                <Cell className="tabular-nums">{g.detail_retention_days} días</Cell>
              </Row>
            ))}
          </TableBody>
        </DataTable>
    </section>
  )
}
