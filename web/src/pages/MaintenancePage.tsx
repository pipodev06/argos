import { useState } from 'react'
import { WrenchIcon } from 'lucide-react'
import { TableError } from '@/components/data-table/TableStates'
import { Button } from '@/components/ui/button'
import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import { EmptyState } from '@/components/ui/empty-state'
import { PageHeader } from '@/components/ui/page-header'
import { Skeleton } from '@/components/ui/skeleton'
import { MaintenanceFormDialog } from '@/features/alerts/components/MaintenanceFormDialog'
import { MaintenanceTable, type MaintenanceAction } from '@/features/alerts/components/MaintenanceTable'
import { useMaintenanceList } from '@/features/alerts/hooks/useAlerts'
import { deleteMaintenance } from '@/features/alerts/services/alerts.service'
import type { MaintenanceWindow } from '@/features/alerts/types/alert.types'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { useUserMutation } from '@/features/users/hooks/useUserMutation'

export function MaintenancePage() {
  const { user } = useAuth()
  const isAdmin = user?.role === 'admin'
  const { data: windows, error, loading, refresh } = useMaintenanceList()
  const [form, setForm] = useState<{ window: MaintenanceWindow | null } | null>(null)
  const [deleting, setDeleting] = useState<MaintenanceWindow | null>(null)
  const del = useUserMutation()

  function handleAction(action: MaintenanceAction, window: MaintenanceWindow) {
    if (action === 'edit') setForm({ window })
    else setDeleting(window)
  }

  async function confirmDelete() {
    if (!deleting) return
    if (await del.run(() => deleteMaintenance(deleting.id))) {
      setDeleting(null)
      void refresh()
    }
  }

  return (
    <>
      <PageHeader
        title="Mantenimiento"
        actions={isAdmin && <Button size="sm" onClick={() => setForm({ window: null })}>Nueva ventana</Button>}
      />
      <div className="flex flex-col gap-3" aria-busy={loading}>
        {error && <TableError message={error} onRetry={() => void refresh()} />}
        {loading && !windows && <Skeleton className="h-40 w-full" />}
        {windows && windows.length === 0 && (
          <EmptyState
            Icon={WrenchIcon}
            title="Sin ventanas"
          />
        )}
        {windows && windows.length > 0 && <MaintenanceTable windows={windows} canAct={isAdmin} onAction={handleAction} />}
      </div>
      {isAdmin && (
        <>
          <MaintenanceFormDialog
            window={form?.window ?? null}
            open={form !== null}
            onClose={() => setForm(null)}
            onSaved={() => {
              setForm(null)
              void refresh()
            }}
          />
          <ConfirmDialog
            open={deleting !== null}
            title={`Eliminar "${deleting?.name ?? ''}"`}
            description="La ventana se elimina y las alertas suprimidas vuelven a notificar."
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
