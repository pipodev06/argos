import { useEffect, useState } from 'react'
import { DownloadIcon, RouterIcon } from 'lucide-react'
import { NoResultsState, TableError } from '@/components/data-table/TableStates'
import { FilterBar } from '@/components/filters/FilterBar'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/ui/empty-state'
import { InlineError } from '@/components/ui/inline-error'
import { PageHeader } from '@/components/ui/page-header'
import { Pagination } from '@/components/ui/pagination'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { DeleteDeviceConfirm } from '@/features/devices/components/DeleteDeviceConfirm'
import { DeviceFormDialog } from '@/features/devices/components/DeviceFormDialog'
import { DeviceTable, type DeviceAction } from '@/features/devices/components/DeviceTable'
import { useDeviceEnabled } from '@/features/devices/hooks/useDeviceEnabled'
import { useDeviceList } from '@/features/devices/hooks/useDevices'
import { devicesCsvUrl } from '@/features/devices/services/devices.service'
import type { Device } from '@/features/devices/types/device.types'
import { buildDeviceCsvQuery, buildDeviceQuery, DEVICE_FIELDS } from '@/features/devices/utils/deviceQuery'
import { useCursorPagination } from '@/hooks/useCursorPagination'
import { useUrlFilters } from '@/hooks/useUrlFilters'
import type { PageSize } from '@/lib/pagination'

export function DevicesPage() {
  const { user } = useAuth()
  const isAdmin = user?.role === 'admin'
  const pagination = useCursorPagination()
  const resetPagination = pagination.reset
  const { filters, setFilters, clear, hasActive } = useUrlFilters(DEVICE_FIELDS, { onChange: resetPagination })
  const { data, error, errorStatus, loading, refresh } = useDeviceList(buildDeviceQuery(filters, pagination.cursor))
  const [form, setForm] = useState<{ device: Device | null } | null>(null)
  const [deleting, setDeleting] = useState<Device | null>(null)
  const enabling = useDeviceEnabled(() => void refresh())

  // El cursor es opaco y se invalida al reiniciar el servidor (400 invalid_cursor): se vuelve a la 1a pagina.
  const staleCursor = errorStatus === 400 && pagination.cursor !== null
  useEffect(() => {
    if (staleCursor) resetPagination()
  }, [staleCursor, resetPagination])

  function handleAction(action: DeviceAction, device: Device) {
    if (action === 'edit') setForm({ device })
    else if (action === 'toggle') void enabling.toggle(device)
    else setDeleting(device)
  }

  const empty = data !== null && data.total === 0 && !hasActive
  const noResults = data !== null && data.total === 0 && hasActive
  const showTable = !empty && !noResults && !(error && data === null)

  return (
    <>
      <PageHeader
        title="Dispositivos"
        actions={
          <>
            <Button asChild variant="outline" size="sm">
              <a href={devicesCsvUrl(buildDeviceCsvQuery(filters))} download>
                <DownloadIcon strokeWidth={1.5} aria-hidden />
                Exportar CSV
              </a>
            </Button>
            {isAdmin && <Button size="sm" onClick={() => setForm({ device: null })}>Nuevo dispositivo</Button>}
          </>
        }
      />
      <div className="flex flex-col gap-4" aria-busy={loading}>
        <FilterBar
          fields={DEVICE_FIELDS}
          values={filters}
          onChange={setFilters}
          onClear={clear}
          itemLabel="dispositivos"
          total={data?.total ?? null}
          overall={null}
        />
        {error && <TableError message={error} onRetry={() => void refresh()} />}
        {enabling.error && <InlineError>{enabling.error}</InlineError>}
        {noResults && <NoResultsState Icon={RouterIcon} onClear={clear} />}
        {empty && (
          <EmptyState
            Icon={RouterIcon}
            title="Sin dispositivos"
          />
        )}
        {showTable && (
          <>
            <DeviceTable
              devices={data?.items ?? null}
              page={pagination.page}
              pageSize={filters.limit}
              isAdmin={isAdmin}
              onAction={handleAction}
            />
            <Pagination
              page={pagination.page}
              pageSize={filters.limit}
              count={data?.items.length ?? 0}
              total={data?.total}
              itemLabel="dispositivos"
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
      {isAdmin && (
        <>
          <DeviceFormDialog
            device={form?.device ?? null}
            open={form !== null}
            onClose={() => setForm(null)}
            onSaved={() => {
              setForm(null)
              void refresh()
            }}
          />
          <DeleteDeviceConfirm
            device={deleting}
            onClose={() => setDeleting(null)}
            onDeleted={() => {
              setDeleting(null)
              void refresh()
            }}
          />
        </>
      )}
    </>
  )
}
