import { useState } from 'react'
import { IconKey } from '@tabler/icons-react'
import { TableError } from '@/components/data-table/TableStates'
import { Button } from '@/components/ui/button'
import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import { EmptyState } from '@/components/ui/empty-state'
import { PageHeader } from '@/components/ui/page-header'
import { Skeleton } from '@/components/ui/skeleton'
import { SnmpProfileFormDialog } from '@/features/snmp-profiles/components/SnmpProfileFormDialog'
import { SnmpProfilesTable, type SnmpProfileAction } from '@/features/snmp-profiles/components/SnmpProfilesTable'
import { useSnmpProfiles } from '@/features/snmp-profiles/hooks/useSnmpProfiles'
import { deleteSnmpProfile } from '@/features/snmp-profiles/services/snmpProfiles.service'
import type { SnmpProfile } from '@/features/snmp-profiles/types/snmpProfile.types'
import { mapSnmpProfileError } from '@/features/snmp-profiles/utils/errors'
import { useUserMutation } from '@/features/users/hooks/useUserMutation'

// Página solo de admin (el ítem del menú y la API lo exigen).
export function SnmpProfilesPage() {
  const { data: profiles, error, loading, refresh } = useSnmpProfiles()
  const [form, setForm] = useState<{ profile: SnmpProfile | null } | null>(null)
  const [deleting, setDeleting] = useState<SnmpProfile | null>(null)
  const del = useUserMutation()

  function handleAction(action: SnmpProfileAction, profile: SnmpProfile) {
    if (action === 'edit') setForm({ profile })
    else setDeleting(profile)
  }

  async function confirmDelete() {
    if (!deleting) return
    if (await del.run(() => deleteSnmpProfile(deleting.id), mapSnmpProfileError)) {
      setDeleting(null)
      void refresh()
    }
  }

  return (
    <>
      <PageHeader title="Perfiles SNMP" actions={<Button size="sm" onClick={() => setForm({ profile: null })}>Nuevo perfil</Button>} />
      <div className="flex flex-col gap-3" aria-busy={loading}>
        {error && <TableError message={error} onRetry={() => void refresh()} />}
        {loading && !profiles && <Skeleton className="h-40 w-full" />}
        {profiles && profiles.length === 0 && (
          <EmptyState Icon={IconKey} title="Sin perfiles SNMP" description='Crea el primero con "Nuevo perfil" y asígnalo a un dispositivo.' />
        )}
        {profiles && profiles.length > 0 && <SnmpProfilesTable profiles={profiles} onAction={handleAction} />}
      </div>
      <SnmpProfileFormDialog
        profile={form?.profile ?? null}
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
        description="El perfil se elimina de forma definitiva. No se puede si hay dispositivos que lo usan."
        confirmLabel="Eliminar"
        busy={del.busy}
        error={del.error}
        onConfirm={() => void confirmDelete()}
        onClose={() => {
          del.clearError()
          setDeleting(null)
        }}
      />
    </>
  )
}
