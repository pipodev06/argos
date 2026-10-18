import { useState } from 'react'
import { Link, useNavigate } from 'react-router'
import { ActivityIcon } from 'lucide-react'
import { NoResultsState, TableError } from '@/components/data-table/TableStates'
import { FilterBar } from '@/components/filters/FilterBar'
import { Button } from '@/components/ui/button'
import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import { EmptyState } from '@/components/ui/empty-state'
import { InlineError } from '@/components/ui/inline-error'
import { PageHeader } from '@/components/ui/page-header'
import { Skeleton } from '@/components/ui/skeleton'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { ChecksTable, type CheckAction } from '@/features/checks/components/ChecksTable'
import { useServiceChecks } from '@/features/checks/hooks/useChecks'
import { deleteCheck, updateCheck } from '@/features/checks/services/checks.service'
import type { ServiceCheck } from '@/features/checks/types/check.types'
import { CHECK_FIELDS, filterChecks } from '@/features/checks/utils/checkQuery'
import { useAllDevices } from '@/features/devices/hooks/useDevices'
import { useUserMutation } from '@/features/users/hooks/useUserMutation'
import { useUrlFilters } from '@/hooks/useUrlFilters'

export function ChecksPage() {
  const { user } = useAuth()
  const isAdmin = user?.role === 'admin'
  const { data: checks, error, loading, refresh } = useServiceChecks()
  const { data: devices } = useAllDevices()
  const { filters, setFilters, clear, hasActive } = useUrlFilters(CHECK_FIELDS)
  const navigate = useNavigate()
  const [deleting, setDeleting] = useState<ServiceCheck | null>(null)
  const del = useUserMutation()
  const toggle = useUserMutation()

  const shown = checks ? filterChecks(checks, filters) : null

  function handleAction(action: CheckAction, check: ServiceCheck) {
    if (action === 'edit') navigate(`/chequeos/${check.id}`)
    else if (action === 'delete') setDeleting(check)
    else void toggle.run(() => updateCheck(check.id, { enabled: !check.enabled })).then((ok) => ok && void refresh())
  }

  async function confirmDelete() {
    if (!deleting) return
    if (await del.run(() => deleteCheck(deleting.id))) {
      setDeleting(null)
      void refresh()
    }
  }

  return (
    <>
      <PageHeader
        title="Chequeos"
        actions={
          isAdmin && (
            <Button asChild size="sm">
              <Link to="/chequeos/nuevo">Nuevo chequeo</Link>
            </Button>
          )
        }
      />
      <div className="flex flex-col gap-4" aria-busy={loading}>
        <FilterBar
          fields={CHECK_FIELDS}
          values={filters}
          onChange={setFilters}
          onClear={clear}
        />
        {error && <TableError message={error} onRetry={() => void refresh()} />}
        {toggle.error && <InlineError>{toggle.error}</InlineError>}
        {loading && !checks && <Skeleton className="h-40 w-full" />}
        {checks && checks.length === 0 && (
          <EmptyState
            Icon={ActivityIcon}
            title="Sin chequeos"
          />
        )}
        {checks && checks.length > 0 && shown?.length === 0 && hasActive && (
          <NoResultsState Icon={ActivityIcon} onClear={clear} />
        )}
        {shown && shown.length > 0 && <ChecksTable checks={shown} canAct={isAdmin} busy={toggle.busy} devices={devices ?? undefined} onAction={handleAction} />}
      </div>
      {isAdmin && (
        <>
          <ConfirmDialog
            open={deleting !== null}
            title={`Eliminar "${deleting?.name ?? ''}"`}
            description="El chequeo se elimina de forma definitiva, junto con su estado actual en los hosts y las reglas de alerta que lo usan."
            confirmLabel="Eliminar"
            destructive
            busy={del.busy}
            error={del.error}
            onConfirm={() => void confirmDelete()}
            onClose={() => {
              del.clearError()
              setDeleting(null)
            }}
          />
        </>
      )}
    </>
  )
}
