import { useEffect, useState } from 'react'
import { BellIcon } from 'lucide-react'
import { NoResultsState, TableError } from '@/components/data-table/TableStates'
import { FilterBar } from '@/components/filters/FilterBar'
import { EmptyState } from '@/components/ui/empty-state'
import { PageHeader } from '@/components/ui/page-header'
import { Pagination } from '@/components/ui/pagination'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { AlertActionDialog, type AlertActionKind } from '@/features/alerts/components/AlertActionDialog'
import { AlertsTable } from '@/features/alerts/components/AlertsTable'
import { useAlertList } from '@/features/alerts/hooks/useAlerts'
import type { Alert } from '@/features/alerts/types/alert.types'
import { ALERT_FIELDS } from '@/features/alerts/utils/alertQuery'
import { canAckAlerts, canResolveAlerts } from '@/features/alerts/utils/permissions'
import { useCursorPagination } from '@/hooks/useCursorPagination'
import { useNow } from '@/hooks/useNow'
import { useUrlFilters } from '@/hooks/useUrlFilters'
import type { PageSize } from '@/lib/pagination'

export function AlertsPage() {
  const { user } = useAuth()
  const pagination = useCursorPagination()
  const resetPagination = pagination.reset
  const { filters, setFilters, clear, hasActive } = useUrlFilters(ALERT_FIELDS, { onChange: resetPagination })
  const { data, error, errorStatus, loading, refresh } = useAlertList(filters, pagination.cursor)
  const now = useNow(30_000)
  const [target, setTarget] = useState<{ action: AlertActionKind; alert: Alert } | null>(null)

  // El cursor se invalida al reiniciar el servidor (400 invalid_cursor): se vuelve a la 1a pagina.
  const staleCursor = errorStatus === 400 && pagination.cursor !== null
  useEffect(() => {
    if (staleCursor) resetPagination()
  }, [staleCursor, resetPagination])

  const noAlerts = data !== null && data.total === 0 && !hasActive
  const noMatch = data !== null && data.items.length === 0 && hasActive
  const showTable = !noAlerts && !noMatch && !(error && data === null)

  return (
    <>
      <PageHeader title="Alertas" />
      <div className="flex flex-col gap-4" aria-busy={loading}>
        <FilterBar
          fields={ALERT_FIELDS}
          values={filters}
          onChange={setFilters}
          onClear={clear}
        />
        {error && <TableError message={error} onRetry={() => void refresh()} />}
        {noAlerts && (
          <EmptyState
            Icon={BellIcon}
            title="Sin alertas"
          />
        )}
        {noMatch && <NoResultsState Icon={BellIcon} onClear={clear} />}
        {showTable && (
          <>
            <AlertsTable
              alerts={data?.items ?? null}
              page={pagination.page}
              pageSize={filters.limit}
              now={now}
              canAck={canAckAlerts(user)}
              canResolve={canResolveAlerts(user)}
              onAction={(action, alert) => setTarget({ action, alert })}
            />
            <Pagination
              page={pagination.page}
              pageSize={filters.limit}
              count={data?.items.length ?? 0}
              total={data?.total}
              itemLabel="alertas"
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
      <AlertActionDialog
        target={target}
        onClose={() => {
          setTarget(null)
          void refresh()
        }}
      />
    </>
  )
}
