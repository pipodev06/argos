import { useEffect } from 'react'
import { IconHistory, IconLock, IconRefresh } from '@tabler/icons-react'
import { NoResultsState, TableError } from '@/components/data-table/TableStates'
import { FilterBar } from '@/components/filters/FilterBar'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/ui/empty-state'
import { PageHeader } from '@/components/ui/page-header'
import { Pagination } from '@/components/ui/pagination'
import { AuditTable } from '@/features/audit/components/AuditTable'
import { useAuditList } from '@/features/audit/hooks/useAudit'
import { AUDIT_FIELDS, FILTER_ERROR } from '@/features/audit/utils/auditQuery'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { useCursorPagination } from '@/hooks/useCursorPagination'
import { useUrlFilters } from '@/hooks/useUrlFilters'
import type { PageSize } from '@/lib/pagination'

/** Solo administradores: un viewer ve el aviso y no se monta nada que llame a la API. */
export function AuditPage() {
  const { user } = useAuth()
  if (user?.role !== 'admin') {
    return (
      <>
        <PageHeader title="Auditoría" />
        <EmptyState Icon={IconLock} title="No tienes permiso" description="Solo los administradores consultan la auditoría." />
      </>
    )
  }
  return <AuditAdmin />
}

function AuditAdmin() {
  const pagination = useCursorPagination()
  const resetPagination = pagination.reset
  const { filters, setFilters, clear, hasActive } = useUrlFilters(AUDIT_FIELDS, { onChange: resetPagination })
  const { data, error, errorStatus, loading, refresh } = useAuditList(filters, pagination.cursor)

  // 400 con cursor = cursor invalido (p. ej. tras reiniciar el servidor): se vuelve a la 1a pagina.
  const staleCursor = errorStatus === 400 && pagination.cursor !== null
  useEffect(() => {
    if (staleCursor) resetPagination()
  }, [staleCursor, resetPagination])
  // 400 sin cursor = filtros invalidos (rango de fechas).
  const message = errorStatus === 400 && pagination.cursor === null ? FILTER_ERROR : error

  const noEntries = data !== null && data.total === 0 && !hasActive
  const noMatch = data !== null && data.items.length === 0 && hasActive
  const showTable = !noEntries && !noMatch && !(error && data === null)

  return (
    <>
      <PageHeader
        title="Auditoría"
        actions={
          <Button variant="outline" size="sm" onClick={() => void refresh()} disabled={loading}>
            <IconRefresh stroke={1.5} aria-hidden />
            Actualizar
          </Button>
        }
      />
      <div className="flex flex-col gap-3" aria-busy={loading}>
        <FilterBar
          fields={AUDIT_FIELDS}
          values={filters}
          onChange={setFilters}
          onClear={clear}
          itemLabel="eventos"
          total={data?.total ?? null}
          overall={null}
        />
        {message && <TableError message={message} onRetry={() => void refresh()} />}
        {noEntries && <EmptyState Icon={IconHistory} title="Sin eventos" description="Aún no hay eventos en el registro de auditoría." />}
        {noMatch && <NoResultsState Icon={IconHistory} title="Ningún evento coincide con los filtros." onClear={clear} />}
        {showTable && (
          <>
            <AuditTable entries={data?.items ?? null} />
            <Pagination
              page={pagination.page}
              pageSize={filters.limit}
              count={data?.items.length ?? 0}
              total={data?.total}
              itemLabel="eventos"
              hasPrev={pagination.hasPrev}
              hasNext={data?.next_cursor != null}
              onPrev={pagination.prev}
              onNext={() => data?.next_cursor && pagination.next(data.next_cursor)}
              onPageSizeChange={(size: PageSize) => setFilters({ limit: size })}
              loading={loading}
            />
          </>
        )}
      </div>
    </>
  )
}
