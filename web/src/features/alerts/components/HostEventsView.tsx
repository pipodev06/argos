import { HistoryIcon } from 'lucide-react'
import { Cell, DataTable, HeadCell, HeadRow, Row, RowNumberCell, RowNumberHeadCell } from '@/components/data-table/DataTable'
import { TableError } from '@/components/data-table/TableStates'
import { EmptyState } from '@/components/ui/empty-state'
import { InlineError } from '@/components/ui/inline-error'
import { Pagination } from '@/components/ui/pagination'
import { Skeleton } from '@/components/ui/skeleton'
import { BackToHostsLink } from '@/features/hosts/components/BackToHostsLink'
import { HostStatusBadge } from '@/features/hosts/components/HostStatusBadge'
import { useHostDetail } from '@/features/hosts/hooks/useHostDetail'
import { formatFullDate } from '@/features/hosts/utils/format'
import { useCursorPagination } from '@/hooks/useCursorPagination'
import { rowNumber } from '@/lib/pagination'
import { useHostEventsPage } from '../hooks/useAlerts'
import { HOST_EVENT_SOURCE_LABELS } from '../utils/format'

/** Historial de estado completo de un host, paginado (un laptop que se enciende y apaga genera muchos eventos). */
export function HostEventsView({ id }: { id: string }) {
  const detail = useHostDetail(id)
  const pagination = useCursorPagination()
  const { data, error, errorStatus, loading, refresh } = useHostEventsPage(id, pagination.pageSize, pagination.cursor)
  const notFound = detail.errorStatus === 400 || detail.errorStatus === 404 || errorStatus === 404

  return (
    <div className="flex flex-col gap-6">
      <BackToHostsLink to={`/hosts/${encodeURIComponent(id)}`}>Volver al host</BackToHostsLink>
      {notFound ? (
        <InlineError>Host no encontrado.</InlineError>
      ) : (
        <>
          <h1 className="min-w-0 truncate text-lg font-semibold">
            Historial de estado{detail.data ? ` · ${detail.data.host.hostname}` : ''}
          </h1>
          <div className="flex flex-col gap-3" aria-busy={loading}>
            {error && <TableError message={error} onRetry={() => void refresh()} />}
            {!data && !error && <Skeleton className="h-40 w-full" />}
            {data && data.items.length === 0 && pagination.page === 1 && (
              <EmptyState Icon={HistoryIcon} title="Sin cambios" />
            )}
            {data && data.items.length > 0 && (
              <>
                <DataTable caption="Historial de estado del host" className="min-w-[44rem]">
                  <thead>
                    <HeadRow>
                      <RowNumberHeadCell />
                      <HeadCell>De</HeadCell>
                      <HeadCell>A</HeadCell>
                      <HeadCell>Fecha</HeadCell>
                      <HeadCell>Origen</HeadCell>
                    </HeadRow>
                  </thead>
                  <tbody>
                    {data.items.map((e, i) => (
                      <Row key={e.id}>
                        <RowNumberCell n={rowNumber(pagination.page, pagination.pageSize, i)} />
                        <Cell>
                          {e.from_status === '' ? <span className="text-text-muted">Primer estado</span> : <HostStatusBadge status={e.from_status} />}
                        </Cell>
                        <Cell>
                          <HostStatusBadge status={e.to_status} />
                        </Cell>
                        <Cell className="tabular-nums">{formatFullDate(e.at)}</Cell>
                        <Cell>{HOST_EVENT_SOURCE_LABELS[e.source]}</Cell>
                      </Row>
                    ))}
                  </tbody>
                </DataTable>
                <Pagination
                  page={pagination.page}
                  pageSize={pagination.pageSize}
                  count={data.items.length}
                  itemLabel="cambios de estado"
                  hasPrev={pagination.hasPrev}
                  hasNext={data.next_cursor !== null}
                  onPrev={pagination.prev}
                  onNext={() => data.next_cursor && pagination.next(data.next_cursor)}
                  onPageSizeChange={pagination.setPageSize}
                  loading={loading}
                />
              </>
            )}
          </div>
        </>
      )}
    </div>
  )
}
