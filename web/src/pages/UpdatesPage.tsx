import { useEffect } from 'react'
import { IconShieldCheck } from '@tabler/icons-react'
import { FilterBar } from '@/components/filters/FilterBar'
import { NoResultsState, TableError } from '@/components/data-table/TableStates'
import { EmptyState } from '@/components/ui/empty-state'
import { InlineError } from '@/components/ui/inline-error'
import { PageHeader } from '@/components/ui/page-header'
import { Pagination } from '@/components/ui/pagination'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { FleetTable } from '@/features/updates/components/FleetTable'
import { useFleetUpdates } from '@/features/updates/hooks/useUpdates'
import { defaultDir, FLEET_FIELDS, type SortKey } from '@/features/updates/utils/fleetQuery'
import { canViewUpdates } from '@/features/updates/utils/updates'
import { useCursorPagination } from '@/hooks/useCursorPagination'
import { useNow } from '@/hooks/useNow'
import { useUrlFilters } from '@/hooks/useUrlFilters'
import type { PageSize } from '@/lib/pagination'

export function UpdatesPage() {
  const { user } = useAuth()
  if (!canViewUpdates(user)) {
    return (
      <>
        <PageHeader title="Parches" />
        <InlineError>Parches no disponible.</InlineError>
      </>
    )
  }
  return <FleetUpdates />
}

function FleetUpdates() {
  const now = useNow(60_000)
  const pagination = useCursorPagination()
  const resetPagination = pagination.reset
  const { filters, setFilters, clear, hasActive } = useUrlFilters(FLEET_FIELDS, { onChange: resetPagination })
  const { data, error, errorStatus, loading, refresh } = useFleetUpdates(filters, pagination.cursor)

  // El cursor es opaco y se invalida al reiniciar el servidor (400): se vuelve a la 1a pagina.
  const staleCursor = errorStatus === 400 && pagination.cursor !== null
  useEffect(() => {
    if (staleCursor) resetPagination()
  }, [staleCursor, resetPagination])

  function handleSort(key: SortKey) {
    const dir = key === filters.sort ? (filters.dir === 'asc' ? 'desc' : 'asc') : defaultDir(key)
    setFilters({ sort: key, dir })
  }

  // 404 = sin acceso: no se revela nada mas (ni si existen hosts).
  const denied = errorStatus === 404
  const empty = data !== null && data.items.length === 0 && !hasActive
  const noMatch = data !== null && data.items.length === 0 && hasActive
  const showTable = !denied && !empty && !noMatch && !(error && data === null)

  return (
    <>
      <PageHeader title="Parches" />
      {denied ? (
        <InlineError>Sin acceso a los parches.</InlineError>
      ) : (
        <div className="flex flex-col gap-3" aria-busy={loading}>
          <FilterBar
            fields={FLEET_FIELDS}
            values={filters}
            onChange={setFilters}
            onClear={clear}
            itemLabel="hosts"
            total={data?.total ?? null}
            overall={null}
          />
          {error && <TableError message={error} onRetry={() => void refresh()} />}
          {empty && (
            <EmptyState
              Icon={IconShieldCheck}
              title="Sin datos aún"
              description="Activa el módulo updates en el agente para ver aquí los parches pendientes."
            />
          )}
          {noMatch && <NoResultsState Icon={IconShieldCheck} title="Ningún host coincide con los filtros." onClear={clear} />}
          {showTable && (
            <>
              <FleetTable
                items={data?.items ?? null}
                page={pagination.page}
                pageSize={filters.limit}
                sort={filters.sort}
                dir={filters.dir}
                onSort={handleSort}
                now={now}
              />
              <Pagination
                page={pagination.page}
                pageSize={filters.limit}
                count={data?.items.length ?? 0}
                total={data?.total}
                itemLabel="hosts"
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
      )}
    </>
  )
}
