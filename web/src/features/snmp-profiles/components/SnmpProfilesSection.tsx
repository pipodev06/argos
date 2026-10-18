import { useState } from 'react'
import { KeyRoundIcon } from 'lucide-react'
import { TableError } from '@/components/data-table/TableStates'
import { Button } from '@/components/ui/button'
import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import { EmptyState } from '@/components/ui/empty-state'
import { PageHeader } from '@/components/ui/page-header'
import { Skeleton } from '@/components/ui/skeleton'
import { useUserMutation } from '@/features/users/hooks/useUserMutation'
import { useSnmpProfiles } from '../hooks/useSnmpProfiles'
import { deleteSnmpProfile } from '../services/snmpProfiles.service'
import type { SnmpProfile } from '../types/snmpProfile.types'
import { mapSnmpProfileError } from '../utils/errors'
import { SnmpProfileFormDialog } from './SnmpProfileFormDialog'
import { SnmpProfilesTable, type SnmpProfileAction } from './SnmpProfilesTable'

/** Página Perfiles SNMP (solo admin: la ruta y la API lo exigen). */
export function SnmpProfilesSection() {
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
          <EmptyState Icon={KeyRoundIcon} title="Sin perfiles SNMP" />
        )}
        {profiles && profiles.length > 0 && <SnmpProfilesTable profiles={profiles} onAction={handleAction} />}
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
          destructive
          busy={del.busy}
          error={del.error}
          onConfirm={() => void confirmDelete()}
          onClose={() => {
            del.clearError()
            setDeleting(null)
          }}
        />
      </div>
    </>
  )
}
