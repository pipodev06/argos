import { useEffect, useState } from 'react'
import { DownloadIcon, RefreshCwIcon, ServerIcon } from 'lucide-react'
import { FilterBar } from '@/components/filters/FilterBar'
import { NoResultsState, TableError } from '@/components/data-table/TableStates'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/ui/empty-state'
import { PageHeader } from '@/components/ui/page-header'
import { Pagination } from '@/components/ui/pagination'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { AddHostDialog } from '@/features/hosts/components/AddHostDialog'
import { BulkBar } from '@/features/hosts/components/BulkBar'
import { ColumnsMenu } from '@/features/hosts/components/ColumnsMenu'
import { PendingTokensDialog } from '@/features/hosts/components/PendingTokensDialog'
import { HostTable } from '@/features/hosts/components/HostTable'
import { useColumnVisibility } from '@/features/hosts/hooks/useColumnVisibility'
import { useHostList } from '@/features/hosts/hooks/useHostList'
import { hostsCsvUrl } from '@/features/hosts/services/hosts.service'
import { buildCsvQuery, defaultDir, HOST_FIELDS, isVolatileSort, type SortKey } from '@/features/hosts/utils/hostQuery'
import { useSummary } from '@/features/summary/hooks/useSummary'
import { useCursorPagination } from '@/hooks/useCursorPagination'
import { useUrlFilters } from '@/hooks/useUrlFilters'
import type { PageSize } from '@/lib/pagination'

export function HostsPage() {
  const { user } = useAuth()
  const isAdmin = user?.role === 'admin'
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set())
  const pagination = useCursorPagination()
  const resetPagination = pagination.reset
  const { filters, setFilters, clear, hasActive } = useUrlFilters(HOST_FIELDS, {
    onChange: () => {
      resetPagination()
      setSelected(new Set())
    },
  })
  const { data, error, errorStatus, loading, refresh } = useHostList(filters, pagination.cursor)
  const summary = useSummary()
  const columns = useColumnVisibility()
  const [addOpen, setAddOpen] = useState(false)
  const [tokensOpen, setTokensOpen] = useState(false)

  // El cursor es opaco y se invalida al reiniciar el servidor (400 invalid_cursor): se vuelve a la 1a pagina.
  const staleCursor = errorStatus === 400 && pagination.cursor !== null
  useEffect(() => {
    if (staleCursor) resetPagination()
  }, [staleCursor, resetPagination])

  function handleSort(key: SortKey) {
    const dir = key === filters.sort ? (filters.dir === 'asc' ? 'desc' : 'asc') : defaultDir(key)
    setFilters({ sort: key, dir })
  }

  function handleSizeChange(size: PageSize) {
    setFilters({ limit: size })
  }

  function toggle(id: string, on: boolean) {
    setSelected((prev) => {
      const next = new Set(prev)
      if (on) next.add(id)
      else next.delete(id)
      return next
    })
  }

  function toggleAll(on: boolean) {
    setSelected((prev) => {
      const next = new Set(prev)
      data?.items.filter((h) => h.kind === 'agent').forEach((h) => (on ? next.add(h.id) : next.delete(h.id)))
      return next
    })
  }

  function afterBulk() {
    setSelected(new Set())
    void refresh()
    void summary.refresh()
  }

  const overall = summary.data
    ? Object.values(summary.data.counts).reduce((a, b) => a + b, 0)
    : null
  const active = hasActive
  const volatile = isVolatileSort(filters.sort)
  const noHosts = data !== null && data.total === 0 && !active
  const noMatch = data !== null && data.items.length === 0 && active
  const showTable = !noHosts && !noMatch && !(error && data === null)

  return (
    <>
      <PageHeader
        title="Hosts"
        actions={
          <>
            {volatile && (
              <Button variant="outline" size="sm" onClick={() => void refresh()}>
                <RefreshCwIcon strokeWidth={1.5} aria-hidden />
                Actualizar
              </Button>
            )}
            <Button asChild variant="outline" size="sm">
              <a href={hostsCsvUrl(buildCsvQuery(filters))} download>
                <DownloadIcon strokeWidth={1.5} aria-hidden />
                Exportar CSV
              </a>
            </Button>
            {isAdmin && (
              <Button variant="outline" size="sm" onClick={() => setTokensOpen(true)}>
                Tokens pendientes
              </Button>
            )}
            {isAdmin && (
              <Button size="sm" onClick={() => setAddOpen(true)}>
                Agregar host
              </Button>
            )}
          </>
        }
      />
      <div className="flex flex-col gap-4" aria-busy={loading}>
        {isAdmin && selected.size > 0 ? (
          <BulkBar ids={[...selected]} onClear={() => setSelected(new Set())} onDone={afterBulk} />
        ) : (
          <FilterBar
            fields={HOST_FIELDS}
            values={filters}
            onChange={setFilters}
            onClear={clear}
            itemLabel="hosts"
            total={data?.total ?? null}
            overall={overall}
            actions={<ColumnsMenu visible={columns.visible} onToggle={columns.toggle} />}
          />
        )}
        {volatile && (
          <p className="text-xs text-text-muted">
            Ordenado por una métrica que cambia: la lista no se reordena sola. Usa "Actualizar".
          </p>
        )}

        {error && <TableError message={error} onRetry={() => void refresh()} />}

        {noHosts && (
          <EmptyState
            Icon={ServerIcon}
            title="Sin hosts"
          />
        )}
        {noMatch && <NoResultsState Icon={ServerIcon} onClear={clear} />}
        {showTable && (
          <>
            <HostTable
              hosts={data?.items ?? null}
              page={pagination.page}
              pageSize={filters.limit}
              visible={columns.visible}
              sort={filters.sort}
              dir={filters.dir}
              onSort={handleSort}
              selected={isAdmin ? selected : null}
              onToggle={toggle}
              onToggleAll={toggleAll}
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
              onPageSizeChange={handleSizeChange}
              loading={loading}
            />
          </>
        )}
      </div>
      {isAdmin && <PendingTokensDialog open={tokensOpen} onClose={() => setTokensOpen(false)} />}
      {isAdmin && (
        <AddHostDialog
          open={addOpen}
          onClose={() => {
            setAddOpen(false)
            void refresh()
          }}
        />
      )}
    </>
  )
}
