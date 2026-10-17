import { useState } from 'react'
import { IconLock, IconWebhook } from '@tabler/icons-react'
import { TableError } from '@/components/data-table/TableStates'
import { Button } from '@/components/ui/button'
import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import { EmptyState } from '@/components/ui/empty-state'
import { InlineError } from '@/components/ui/inline-error'
import { PageHeader } from '@/components/ui/page-header'
import { Skeleton } from '@/components/ui/skeleton'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { ChannelFormDialog } from '@/features/channels/components/ChannelFormDialog'
import { ChannelsTable, type ChannelAction } from '@/features/channels/components/ChannelsTable'
import { useChannelTest } from '@/features/channels/hooks/useChannelTest'
import { useChannels } from '@/features/channels/hooks/useChannels'
import { deleteChannel, updateChannel } from '@/features/channels/services/channels.service'
import type { Channel } from '@/features/channels/types/channel.types'
import { mapChannelError } from '@/features/channels/utils/errors'
import { useUserMutation } from '@/features/users/hooks/useUserMutation'

export function ChannelsPage() {
  const { user } = useAuth()
  const isAdmin = user?.role === 'admin'
  const { data: channels, error, errorStatus, loading, refresh } = useChannels()
  // 403: viewer con acceso restringido a grupos; no es un fallo, se explica sin error rojo.
  const forbidden = errorStatus === 403
  const [form, setForm] = useState<{ channel: Channel | null } | null>(null)
  const [deleting, setDeleting] = useState<Channel | null>(null)
  const del = useUserMutation()
  const toggle = useUserMutation()
  const tester = useChannelTest()

  function handleAction(action: ChannelAction, channel: Channel) {
    if (action === 'edit') setForm({ channel })
    else if (action === 'delete') setDeleting(channel)
    else if (action === 'test') void tester.test(channel.id)
    else void toggle.run(() => updateChannel(channel.id, { enabled: !channel.enabled }), mapChannelError).then((ok) => ok && void refresh())
  }

  async function confirmDelete() {
    if (!deleting) return
    if (await del.run(() => deleteChannel(deleting.id))) {
      setDeleting(null)
      void refresh()
    }
  }

  return (
    <>
      <PageHeader
        title="Canales de notificación"
        actions={isAdmin && <Button size="sm" onClick={() => setForm({ channel: null })}>Nuevo canal</Button>}
      />
      <div className="flex flex-col gap-4" aria-busy={loading}>
        {forbidden && (
          <EmptyState
            Icon={IconLock}
            title="Sin permiso"
          />
        )}
        {error && !forbidden && <TableError message={error} onRetry={() => void refresh()} />}
        {toggle.error && <InlineError>{toggle.error}</InlineError>}
        {loading && !channels && <Skeleton className="h-40 w-full" />}
        {channels && channels.length === 0 && (
          <EmptyState Icon={IconWebhook} title="Sin canales" />
        )}
        {channels && channels.length > 0 && (
          <ChannelsTable
            channels={channels}
            canAct={isAdmin}
            busy={toggle.busy}
            testingId={tester.testingId}
            outcomes={tester.outcomes}
            onAction={handleAction}
          />
        )}
      </div>
      {isAdmin && (
        <>
          <ChannelFormDialog
            channel={form?.channel ?? null}
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
            description="El canal se elimina de forma definitiva y deja de recibir alertas."
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
